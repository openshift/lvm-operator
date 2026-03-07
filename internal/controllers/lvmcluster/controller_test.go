/*
Copyright © 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lvmcluster

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr/testr"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func testLVMCluster(deviceClasses ...lvmv1alpha1.DeviceClass) *lvmv1alpha1.LVMCluster {
	return &lvmv1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-lvmcluster",
			Namespace: testNamespace,
		},
		Spec: lvmv1alpha1.LVMClusterSpec{
			Storage: lvmv1alpha1.Storage{DeviceClasses: deviceClasses},
		},
	}
}

func TestActivePVCsExist_NoPVCs(t *testing.T) {
	r := newFakeReconciler(t)
	// Wrap with interceptor that returns empty list for PVC field queries
	r.Client = newFieldFilteringClient(r.Client, resource.GetStorageClassName("vg1"), nil, nil, nil, nil)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
		Name: "vg1",
	})

	exists, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected false when no PVCs exist, got true")
	}
}

func TestActivePVCsExist_MatchingPVC(t *testing.T) {
	scName := resource.GetStorageClassName("vg1")
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &scName,
		},
	}

	r := newFakeReconciler(t)
	r.Client = newFieldFilteringClient(r.Client, scName, []corev1.PersistentVolumeClaim{pvc}, nil, nil, nil)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
		Name: "vg1",
	})

	exists, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected true when matching PVC exists, got false")
	}
}

func TestActivePVCsExist_NonMatchingPVC(t *testing.T) {
	r := newFakeReconciler(t)
	// No matching PVCs for the target SC name
	r.Client = newFieldFilteringClient(r.Client, resource.GetStorageClassName("vg1"), nil, nil, nil, nil)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
		Name: "vg1",
	})

	exists, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected false when no matching PVC exists, got true")
	}
}

func TestActivePVCsExist_ListError(t *testing.T) {
	r := newFakeReconciler(t)

	expectedErr := fmt.Errorf("list failed")
	r.Client = newFieldFilteringClient(r.Client, resource.GetStorageClassName("vg1"), nil, nil, expectedErr, nil)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
		Name: "vg1",
	})

	_, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
	if err == nil {
		t.Error("expected error to be propagated, got nil")
	}
}

func TestRetainPVsExist_NoRetainPolicy(t *testing.T) {
	r := newFakeReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	// All device classes have Delete policy (nil StorageClassOptions = default Delete)
	cluster := testLVMCluster(
		lvmv1alpha1.DeviceClass{Name: "vg1"},
		lvmv1alpha1.DeviceClass{
			Name: "vg2",
			StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
				ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimDelete),
			},
		},
	)

	exists, err := r.retainPVsExistForCluster(ctx, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected false when no Retain policy exists, got true")
	}
}

func TestRetainPVsExist_RetainWithPVs(t *testing.T) {
	scName := resource.GetStorageClassName("vg1")
	pv := corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pv",
		},
		Spec: corev1.PersistentVolumeSpec{
			StorageClassName: scName,
		},
	}

	r := newFakeReconciler(t)
	r.Client = newFieldFilteringClient(r.Client, scName, nil, []corev1.PersistentVolume{pv}, nil, nil)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
		Name: "vg1",
		StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
			ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimRetain),
		},
	})

	exists, err := r.retainPVsExistForCluster(ctx, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected true when Retain PVs exist, got false")
	}
}

func TestRetainPVsExist_RetainNoPVs(t *testing.T) {
	r := newFakeReconciler(t)
	scName := resource.GetStorageClassName("vg1")
	r.Client = newFieldFilteringClient(r.Client, scName, nil, nil, nil, nil)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
		Name: "vg1",
		StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
			ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimRetain),
		},
	})

	exists, err := r.retainPVsExistForCluster(ctx, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected false when no Retain PVs exist, got true")
	}
}

func TestRetainPVsExist_ListError(t *testing.T) {
	r := newFakeReconciler(t)
	scName := resource.GetStorageClassName("vg1")
	expectedErr := fmt.Errorf("PV list failed")
	r.Client = newFieldFilteringClient(r.Client, scName, nil, nil, nil, expectedErr)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
		Name: "vg1",
		StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
			ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimRetain),
		},
	})

	_, err := r.retainPVsExistForCluster(ctx, cluster)
	if err == nil {
		t.Error("expected error to be propagated, got nil")
	}
}

// newFieldFilteringClient wraps a base client with an interceptor that simulates
// field-indexed List for PVCs and PVs. The fake client does not support MatchingFields
// natively; this interceptor intercepts List calls that carry a field selector for
// "spec.storageClassName" and returns pre-configured results.
func newFieldFilteringClient(
	base client.Client,
	targetSCName string,
	pvcs []corev1.PersistentVolumeClaim,
	pvs []corev1.PersistentVolume,
	pvcListErr error,
	pvListErr error,
) client.Client {
	// The fake client implements client.WithWatch, which interceptor.NewClient requires.
	watchBase := base.(client.WithWatch)
	expectedSelector := fmt.Sprintf("spec.storageClassName=%s", targetSCName)
	return interceptor.NewClient(watchBase, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			listOpts := &client.ListOptions{}
			for _, opt := range opts {
				opt.ApplyToList(listOpts)
			}

			hasFieldSelector := listOpts.FieldSelector != nil && listOpts.FieldSelector.String() != ""

			switch typedList := list.(type) {
			case *corev1.PersistentVolumeClaimList:
				if !hasFieldSelector {
					return c.List(ctx, list, opts...)
				}
				if pvcListErr != nil {
					return pvcListErr
				}
				if strings.Contains(listOpts.FieldSelector.String(), expectedSelector) {
					typedList.Items = pvcs
				} else {
					typedList.Items = nil
				}
				return nil

			case *corev1.PersistentVolumeList:
				if !hasFieldSelector {
					return c.List(ctx, list, opts...)
				}
				if pvListErr != nil {
					return pvListErr
				}
				if strings.Contains(listOpts.FieldSelector.String(), expectedSelector) {
					typedList.Items = pvs
				} else {
					typedList.Items = nil
				}
				return nil

			default:
				return c.List(ctx, list, opts...)
			}
		},
	})
}
