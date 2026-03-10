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

package resource

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr/testr"
	configv1 "github.com/openshift/api/config/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/logpassthrough"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// fakeReconciler implements the resource.Reconciler interface for unit tests.
type fakeReconciler struct {
	client.Client
	scheme    *runtime.Scheme
	namespace string
}

func (f *fakeReconciler) GetNamespace() string          { return f.namespace }
func (f *fakeReconciler) GetImageName() string          { return "test-image" }
func (f *fakeReconciler) SnapshotsEnabled() bool        { return false }
func (f *fakeReconciler) GetVGManagerCommand() []string { return nil }
func (f *fakeReconciler) GetTopoLVMLeaderElectionPassthrough() configv1.LeaderElection {
	return configv1.LeaderElection{}
}
func (f *fakeReconciler) GetLogPassthroughOptions() *logpassthrough.Options {
	return logpassthrough.NewOptions()
}
func (f *fakeReconciler) Scheme() *runtime.Scheme { return f.scheme }

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme, err := lvmv1alpha1.SchemeBuilder.Build()
	if err != nil {
		t.Fatalf("building scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding corev1 to scheme: %v", err)
	}
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding storagev1 to scheme: %v", err)
	}
	return scheme
}

func newFakeStorageClassReconciler(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) *fakeReconciler {
	t.Helper()
	return &fakeReconciler{
		Client:    fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
		scheme:    scheme,
		namespace: "default",
	}
}

func testCluster(deviceClasses ...lvmv1alpha1.DeviceClass) *lvmv1alpha1.LVMCluster {
	return &lvmv1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-lvmcluster",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: lvmv1alpha1.LVMClusterSpec{
			Storage: lvmv1alpha1.Storage{DeviceClasses: deviceClasses},
		},
	}
}

func TestGetTopolvmStorageClasses_Defaults(t *testing.T) {
	scheme := newTestScheme(t)
	r := newFakeStorageClassReconciler(t, scheme)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
	})

	sc := topolvmStorageClass{}
	result := sc.getTopolvmStorageClasses(r, ctx, cluster)

	if len(result) != 1 {
		t.Fatalf("expected 1 StorageClass, got %d", len(result))
	}

	got := result[0]

	// TypeMeta must be set for SSA
	if got.APIVersion != "storage.k8s.io/v1" {
		t.Errorf("expected APIVersion storage.k8s.io/v1, got %s", got.APIVersion)
	}
	if got.Kind != "StorageClass" {
		t.Errorf("expected Kind StorageClass, got %s", got.Kind)
	}

	// Defaults
	if got.ReclaimPolicy == nil || *got.ReclaimPolicy != corev1.PersistentVolumeReclaimDelete {
		t.Errorf("expected Delete reclaim policy, got %v", got.ReclaimPolicy)
	}
	if got.VolumeBindingMode == nil || *got.VolumeBindingMode != storagev1.VolumeBindingWaitForFirstConsumer {
		t.Errorf("expected WaitForFirstConsumer, got %v", got.VolumeBindingMode)
	}

	// Parameters
	if got.Parameters[constants.DeviceClassKey] != "vg1" {
		t.Errorf("expected device class param vg1, got %s", got.Parameters[constants.DeviceClassKey])
	}
	if got.Parameters[constants.FsTypeKey] != "xfs" {
		t.Errorf("expected fstype xfs, got %s", got.Parameters[constants.FsTypeKey])
	}

	// Default annotation should be "false" (no Default flag set on device class)
	if got.Annotations[defaultSCAnnotation] != "false" {
		t.Errorf("expected default annotation false, got %s", got.Annotations[defaultSCAnnotation])
	}
}

func TestGetTopolvmStorageClasses_CustomOptions(t *testing.T) {
	scheme := newTestScheme(t)
	r := newFakeStorageClassReconciler(t, scheme)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	retainPolicy := corev1.PersistentVolumeReclaimRetain
	immediateMode := storagev1.VolumeBindingImmediate

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
		StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
			ReclaimPolicy:     &retainPolicy,
			VolumeBindingMode: &immediateMode,
		},
	})

	sc := topolvmStorageClass{}
	result := sc.getTopolvmStorageClasses(r, ctx, cluster)

	if len(result) != 1 {
		t.Fatalf("expected 1 StorageClass, got %d", len(result))
	}

	got := result[0]

	if got.ReclaimPolicy == nil || *got.ReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		t.Errorf("expected Retain, got %v", got.ReclaimPolicy)
	}
	if got.VolumeBindingMode == nil || *got.VolumeBindingMode != storagev1.VolumeBindingImmediate {
		t.Errorf("expected Immediate, got %v", got.VolumeBindingMode)
	}
}

func TestGetTopolvmStorageClasses_AdditionalParams(t *testing.T) {
	scheme := newTestScheme(t)
	r := newFakeStorageClassReconciler(t, scheme)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
		StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
			AdditionalParameters: map[string]string{
				"custom-key": "custom-value",
				// Attempt to override LVMS-owned keys — these should be overwritten
				constants.DeviceClassKey: "should-be-overridden",
				constants.FsTypeKey:      "should-be-overridden",
			},
		},
	})

	sc := topolvmStorageClass{}
	result := sc.getTopolvmStorageClasses(r, ctx, cluster)

	got := result[0]

	// User key should be present
	if got.Parameters["custom-key"] != "custom-value" {
		t.Errorf("expected custom-key=custom-value, got %s", got.Parameters["custom-key"])
	}
	// LVMS-owned keys should NOT be overwritten
	if got.Parameters[constants.DeviceClassKey] != "vg1" {
		t.Errorf("LVMS-owned key %s should not be overridden, got %s", constants.DeviceClassKey, got.Parameters[constants.DeviceClassKey])
	}
	if got.Parameters[constants.FsTypeKey] != "xfs" {
		t.Errorf("LVMS-owned key %s should not be overridden, got %s", constants.FsTypeKey, got.Parameters[constants.FsTypeKey])
	}
}

func TestGetTopolvmStorageClasses_AdditionalLabels(t *testing.T) {
	scheme := newTestScheme(t)
	r := newFakeStorageClassReconciler(t, scheme)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
		StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
			AdditionalLabels: map[string]string{
				"team":             "storage",
				labels.OwnedByName: "should-be-overridden",
			},
		},
	})

	sc := topolvmStorageClass{}
	result := sc.getTopolvmStorageClasses(r, ctx, cluster)

	got := result[0]

	// User label should be present
	if got.Labels["team"] != "storage" {
		t.Errorf("expected label team=storage, got %s", got.Labels["team"])
	}
	// Managed labels should be set and NOT overwritten by user labels
	if got.Labels[labels.OwnedByName] != "test-lvmcluster" {
		t.Errorf("managed label %s should be %s, got %s", labels.OwnedByName, "test-lvmcluster", got.Labels[labels.OwnedByName])
	}
	if got.Labels[labels.OwnedByNamespace] != "default" {
		t.Errorf("managed label %s should be set, got %s", labels.OwnedByNamespace, got.Labels[labels.OwnedByNamespace])
	}
}

func TestGetTopolvmStorageClasses_DefaultAnnotation(t *testing.T) {
	scheme := newTestScheme(t)

	tests := []struct {
		name           string
		defaultFlag    bool
		existingSCs    []client.Object
		expectedResult string
	}{
		{
			name:           "default=true, no other default SC",
			defaultFlag:    true,
			existingSCs:    nil,
			expectedResult: "true",
		},
		{
			name:           "default=false",
			defaultFlag:    false,
			existingSCs:    nil,
			expectedResult: "false",
		},
		{
			name:        "default=true but another SC is already default",
			defaultFlag: true,
			existingSCs: []client.Object{
				&storagev1.StorageClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-sc",
						Annotations: map[string]string{
							defaultSCAnnotation: "true",
						},
					},
					Provisioner: "other.csi.driver",
				},
			},
			expectedResult: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFakeStorageClassReconciler(t, scheme, tt.existingSCs...)
			ctx := log.IntoContext(context.Background(), testr.New(t))

			cluster := testCluster(lvmv1alpha1.DeviceClass{
				Name:           "vg1",
				FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
				Default:        tt.defaultFlag,
			})

			sc := topolvmStorageClass{}
			result := sc.getTopolvmStorageClasses(r, ctx, cluster)

			got := result[0]
			if got.Annotations[defaultSCAnnotation] != tt.expectedResult {
				t.Errorf("expected default annotation %s, got %s", tt.expectedResult, got.Annotations[defaultSCAnnotation])
			}
		})
	}
}

func TestEnsureCreated_SSAPatch(t *testing.T) {
	scheme := newTestScheme(t)

	var capturedPatchOpts []client.PatchOption
	var capturedPatchType types.PatchType

	interceptClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				capturedPatchOpts = opts
				capturedPatchType = patch.Type()
				// Return nil to simulate a successful patch
				return nil
			},
		}).
		Build()

	r := &fakeReconciler{
		Client:    interceptClient,
		scheme:    scheme,
		namespace: "default",
	}
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
	})

	sc := topolvmStorageClass{}
	err := sc.EnsureCreated(r, ctx, cluster)
	if err != nil {
		t.Fatalf("EnsureCreated returned error: %v", err)
	}

	// Verify SSA patch type
	if capturedPatchType != types.ApplyPatchType {
		t.Errorf("expected patch type %v (Apply), got %v", types.ApplyPatchType, capturedPatchType)
	}

	// Apply captured options to a PatchOptions to verify FieldOwner and Force
	patchOpts := &client.PatchOptions{}
	for _, opt := range capturedPatchOpts {
		opt.ApplyToPatch(patchOpts)
	}

	if patchOpts.FieldManager != storageClassFieldOwner {
		t.Errorf("expected FieldManager %q, got %q", storageClassFieldOwner, patchOpts.FieldManager)
	}
	if patchOpts.Force == nil || *patchOpts.Force != true {
		t.Error("expected Force to be true (ForceOwnership)")
	}
}

func TestEnsureDeleted_NotFound(t *testing.T) {
	scheme := newTestScheme(t)
	r := newFakeStorageClassReconciler(t, scheme)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
	})

	sc := topolvmStorageClass{}
	err := sc.EnsureDeleted(r, ctx, cluster)
	if err != nil {
		t.Errorf("expected no error when SC not found, got: %v", err)
	}
}

func TestEnsureDeleted_Exists(t *testing.T) {
	scheme := newTestScheme(t)
	existingSC := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetStorageClassName("vg1"),
		},
		Provisioner: constants.TopolvmCSIDriverName,
	}
	r := newFakeStorageClassReconciler(t, scheme, existingSC)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
	})

	sc := topolvmStorageClass{}
	err := sc.EnsureDeleted(r, ctx, cluster)
	if err != nil {
		t.Errorf("expected no error during deletion, got: %v", err)
	}

	// Verify SC was deleted
	gotSC := &storagev1.StorageClass{}
	err = r.Get(ctx, types.NamespacedName{Name: GetStorageClassName("vg1")}, gotSC)
	if err == nil {
		t.Error("expected SC to be deleted, but it still exists")
	}
}

func TestEnsureDeleted_DeletionTimestamp(t *testing.T) {
	scheme := newTestScheme(t)
	now := metav1.Now()
	existingSC := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              GetStorageClassName("vg1"),
			DeletionTimestamp: &now,
			Finalizers:        []string{"test-finalizer"}, // needed for fake client to accept DeletionTimestamp
		},
		Provisioner: constants.TopolvmCSIDriverName,
	}
	r := newFakeStorageClassReconciler(t, scheme, existingSC)
	ctx := log.IntoContext(context.Background(), testr.New(t))

	cluster := testCluster(lvmv1alpha1.DeviceClass{
		Name:           "vg1",
		FilesystemType: lvmv1alpha1.FilesystemTypeXFS,
	})

	sc := topolvmStorageClass{}
	err := sc.EnsureDeleted(r, ctx, cluster)
	if err == nil {
		t.Error("expected error when SC has DeletionTimestamp, got nil")
	}

	expectedMsg := fmt.Sprintf("the StorageClass %s is still present, waiting for deletion", GetStorageClassName("vg1"))
	if err.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
	}
}
