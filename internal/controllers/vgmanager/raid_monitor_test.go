package vgmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	lvmmocks "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm/mocks"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func setupRAIDMonitor(t *testing.T) (*RAIDMonitorReconciler, *lvmmocks.MockLVM, client.WithWatch, *events.FakeRecorder) {
	t.Helper()
	mockLVM := lvmmocks.NewMockLVM(t)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node", Labels: map[string]string{
		"kubernetes.io/hostname": "test-host",
	}}}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-lvm-storage"}}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(node, namespace).
		Build()
	fakeRecorder := events.NewFakeRecorder(100)

	reconciler := &RAIDMonitorReconciler{
		Client:        fakeClient,
		Scheme:        scheme.Scheme,
		EventRecorder: fakeRecorder,
		LVM:           mockLVM,
		NodeName:      node.GetName(),
		Namespace:     namespace.GetName(),
	}
	return reconciler, mockLVM, fakeClient, fakeRecorder
}

func TestRAIDMonitorSkipsNonRAIDVG(t *testing.T) {
	ctx := log.IntoContext(context.Background(), zap.New(zap.UseDevMode(true)))
	reconciler, _, fakeClient, _ := setupRAIDMonitor(t)

	vg := &lvmv1alpha1.LVMVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "openshift-lvm-storage"},
		Spec:       lvmv1alpha1.LVMVolumeGroupSpec{},
	}
	if err := fakeClient.Create(ctx, vg); err != nil {
		t.Fatalf("failed to create VG: %v", err)
	}

	result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for non-RAID VG, got %v", result.RequeueAfter)
	}
}

func TestRAIDMonitorDetectsDegradedState(t *testing.T) {
	ctx := log.IntoContext(context.Background(), zap.New(zap.UseDevMode(true)))
	reconciler, mockLVM, fakeClient, _ := setupRAIDMonitor(t)

	vg := &lvmv1alpha1.LVMVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "openshift-lvm-storage"},
		Spec: lvmv1alpha1.LVMVolumeGroupSpec{
			RAIDConfig: &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1},
		},
	}
	if err := fakeClient.Create(ctx, vg); err != nil {
		t.Fatalf("failed to create VG: %v", err)
	}

	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node", Namespace: "openshift-lvm-storage"},
		Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
			LVMVGStatus: []lvmv1alpha1.VGStatus{
				{Name: "vg1", Status: lvmv1alpha1.VGStatusReady},
			},
		},
	}
	if err := fakeClient.Create(ctx, nodeStatus); err != nil {
		t.Fatalf("failed to create node status: %v", err)
	}

	mockLVM.EXPECT().ListVGs(ctx, true).Return([]lvm.VolumeGroup{{
		Name:   "vg1",
		VgSize: "2G",
		PVs: []lvm.PhysicalVolume{
			{PvName: "/dev/sda", VgName: "vg1"},
			{PvName: "[unknown]", VgName: "vg1", PvMissing: "missing"},
		},
	}}, nil).Once()

	mockLVM.EXPECT().ListLVs(ctx, "vg1").Return(&lvm.LVReport{
		Report: []lvm.LVReportItem{{
			Lv: []lvm.LogicalVolume{
				{
					Name:            "lv1",
					VgName:          "vg1",
					LvAttr:          "rwi-a-r---",
					RAIDSyncPercent: "100",
					LVHealthStatus:  "partial",
				},
				{
					Name:            "lv2",
					VgName:          "vg1",
					LvAttr:          "rwi-a-r---",
					RAIDSyncPercent: "100",
					LVHealthStatus:  "",
				},
			},
		}},
	}, nil).Once()

	result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue, got %v", result.RequeueAfter)
	}

	updatedStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(nodeStatus), updatedStatus); err != nil {
		t.Fatalf("failed to get updated status: %v", err)
	}
	if len(updatedStatus.Spec.LVMVGStatus) != 1 {
		t.Fatalf("expected 1 VG status, got %d", len(updatedStatus.Spec.LVMVGStatus))
	}
	if updatedStatus.Spec.LVMVGStatus[0].Status != lvmv1alpha1.VGStatusDegraded {
		t.Errorf("expected VG status Degraded, got %s", updatedStatus.Spec.LVMVGStatus[0].Status)
	}
	if updatedStatus.Spec.LVMVGStatus[0].RAIDStatus == nil {
		t.Fatal("expected RAID status to be set")
	}
	if updatedStatus.Spec.LVMVGStatus[0].RAIDStatus.Status != lvmv1alpha1.RAIDHealthStatusDegraded {
		t.Errorf("expected RAID status Degraded, got %s", updatedStatus.Spec.LVMVGStatus[0].RAIDStatus.Status)
	}
}

func TestRAIDMonitorHandlesLVMFailureGracefully(t *testing.T) {
	ctx := log.IntoContext(context.Background(), zap.New(zap.UseDevMode(true)))
	reconciler, mockLVM, fakeClient, _ := setupRAIDMonitor(t)

	vg := &lvmv1alpha1.LVMVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "openshift-lvm-storage"},
		Spec: lvmv1alpha1.LVMVolumeGroupSpec{
			RAIDConfig: &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1},
		},
	}
	if err := fakeClient.Create(ctx, vg); err != nil {
		t.Fatalf("failed to create VG: %v", err)
	}

	mockLVM.EXPECT().ListVGs(ctx, true).Return(nil, fmt.Errorf("lvm command failed")).Once()

	result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
	if err != nil {
		t.Fatalf("expected nil error on LVM failure, got %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue on LVM failure, got %v", result.RequeueAfter)
	}
}

func TestStatusChanged(t *testing.T) {
	tests := []struct {
		name     string
		old, new *lvmv1alpha1.RAIDStatus
		expected bool
	}{
		{"both nil", nil, nil, false},
		{"old nil new set", nil, &lvmv1alpha1.RAIDStatus{Status: lvmv1alpha1.RAIDHealthStatusHealthy}, true},
		{"old set new nil", &lvmv1alpha1.RAIDStatus{Status: lvmv1alpha1.RAIDHealthStatusHealthy}, nil, true},
		{"same status", &lvmv1alpha1.RAIDStatus{Status: lvmv1alpha1.RAIDHealthStatusHealthy}, &lvmv1alpha1.RAIDStatus{Status: lvmv1alpha1.RAIDHealthStatusHealthy}, false},
		{"different status", &lvmv1alpha1.RAIDStatus{Status: lvmv1alpha1.RAIDHealthStatusHealthy}, &lvmv1alpha1.RAIDStatus{Status: lvmv1alpha1.RAIDHealthStatusDegraded}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusChanged(tt.old, tt.new); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
