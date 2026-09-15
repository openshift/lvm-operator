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
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func newFakeCSIDriverReconciler(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) *fakeReconciler {
	t.Helper()
	return &fakeReconciler{
		Client:    fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
		scheme:    scheme,
		namespace: "default",
	}
}

func getReconciledCSIDriver(t *testing.T, r *fakeReconciler) *storagev1.CSIDriver {
	t.Helper()
	ctx := log.IntoContext(context.Background(), testr.New(t))

	if err := (csiDriver{}).EnsureCreated(r, ctx, testCluster()); err != nil {
		t.Fatalf("EnsureCreated returned error: %v", err)
	}

	got := &storagev1.CSIDriver{}
	key := types.NamespacedName{Name: constants.TopolvmCSIDriverName}
	if err := r.Get(ctx, key, got); err != nil {
		t.Fatalf("getting CSIDriver %q: %v", key.Name, err)
	}
	return got
}

// SELinuxMount tells kubelet it may mount ReadWriteOncePod volumes with "-o context".
func TestCSIDriver_EnsureCreated_SetsSELinuxMount(t *testing.T) {
	scheme := newTestScheme(t)
	got := getReconciledCSIDriver(t, newFakeCSIDriverReconciler(t, scheme))

	if !ptr.Deref(got.Spec.SELinuxMount, false) {
		t.Errorf("expected spec.seLinuxMount to be true, got %v", got.Spec.SELinuxMount)
	}
}

// A cluster upgraded from a release without seLinuxMount already has a CSIDriver, so the
// field has to be reconciled onto the existing object rather than only set at creation.
func TestCSIDriver_EnsureCreated_SetsSELinuxMountOnExistingDriver(t *testing.T) {
	scheme := newTestScheme(t)
	existing := getCSIDriverResource()
	existing.Spec.SELinuxMount = nil

	got := getReconciledCSIDriver(t, newFakeCSIDriverReconciler(t, scheme, existing))

	if !ptr.Deref(got.Spec.SELinuxMount, false) {
		t.Errorf("expected spec.seLinuxMount to be true after upgrade, got %v", got.Spec.SELinuxMount)
	}
}

// The remaining spec fields are immutable, so reconciling an existing driver must leave them
// untouched instead of trying to write them back.
func TestCSIDriver_EnsureCreated_KeepsImmutableFields(t *testing.T) {
	scheme := newTestScheme(t)
	existing := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: constants.TopolvmCSIDriverName},
		Spec: storagev1.CSIDriverSpec{
			AttachRequired:       ptr.To(true),
			PodInfoOnMount:       ptr.To(false),
			VolumeLifecycleModes: []storagev1.VolumeLifecycleMode{storagev1.VolumeLifecycleEphemeral},
		},
	}

	got := getReconciledCSIDriver(t, newFakeCSIDriverReconciler(t, scheme, existing))

	if !ptr.Deref(got.Spec.AttachRequired, false) {
		t.Errorf("spec.attachRequired was overwritten, got %v", got.Spec.AttachRequired)
	}
	if ptr.Deref(got.Spec.PodInfoOnMount, true) {
		t.Errorf("spec.podInfoOnMount was overwritten, got %v", got.Spec.PodInfoOnMount)
	}
	if len(got.Spec.VolumeLifecycleModes) != 1 || got.Spec.VolumeLifecycleModes[0] != storagev1.VolumeLifecycleEphemeral {
		t.Errorf("spec.volumeLifecycleModes was overwritten, got %v", got.Spec.VolumeLifecycleModes)
	}
}
