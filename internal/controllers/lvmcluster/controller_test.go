/*
Copyright © 2026 Red Hat, Inc.

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/logpassthrough"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newGinkgoReconciler(objs ...client.Object) *Reconciler {
	scheme, err := lvmv1alpha1.SchemeBuilder.Build()
	Expect(err).NotTo(HaveOccurred())
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(appsv1.AddToScheme(scheme)).To(Succeed())
	Expect(storagev1.AddToScheme(scheme)).To(Succeed())
	Expect(snapapi.AddToScheme(scheme)).To(Succeed())

	return &Reconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
		Namespace:             testNamespace,
		LogPassthroughOptions: logpassthrough.NewOptions(),
	}
}

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

var _ = Describe("Deletion Gates", func() {

	Describe("activePVCsExistForClusterStorageClasses", func() {
		It("should return false when no PVCs exist", func(ctx context.Context) {
			r := newGinkgoReconciler()
			r.Client = newFieldFilteringClient(r.Client, resource.GetStorageClassName("vg1"), nil, nil, nil, nil)

			cluster := testLVMCluster(lvmv1alpha1.DeviceClass{Name: "vg1"})

			exists, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should return true when a matching PVC exists", func(ctx context.Context) {
			scName := resource.GetStorageClassName("vg1")
			pvc := corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pvc", Namespace: "default"},
				Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &scName},
			}

			r := newGinkgoReconciler()
			r.Client = newFieldFilteringClient(r.Client, scName, []corev1.PersistentVolumeClaim{pvc}, nil, nil, nil)

			cluster := testLVMCluster(lvmv1alpha1.DeviceClass{Name: "vg1"})

			exists, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("should return false when no matching PVC exists", func(ctx context.Context) {
			r := newGinkgoReconciler()
			r.Client = newFieldFilteringClient(r.Client, resource.GetStorageClassName("vg1"), nil, nil, nil, nil)

			cluster := testLVMCluster(lvmv1alpha1.DeviceClass{Name: "vg1"})

			exists, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should propagate list errors", func(ctx context.Context) {
			r := newGinkgoReconciler()
			expectedErr := fmt.Errorf("list failed")
			r.Client = newFieldFilteringClient(r.Client, resource.GetStorageClassName("vg1"), nil, nil, expectedErr, nil)

			cluster := testLVMCluster(lvmv1alpha1.DeviceClass{Name: "vg1"})

			_, err := r.activePVCsExistForClusterStorageClasses(ctx, cluster)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("retainPVsExistForCluster", func() {
		It("should return false when no Retain policy exists", func(ctx context.Context) {
			r := newGinkgoReconciler()

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
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should return true when Retain PVs exist", func(ctx context.Context) {
			scName := resource.GetStorageClassName("vg1")
			pv := corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pv"},
				Spec:       corev1.PersistentVolumeSpec{StorageClassName: scName},
			}

			r := newGinkgoReconciler()
			r.Client = newFieldFilteringClient(r.Client, scName, nil, []corev1.PersistentVolume{pv}, nil, nil)

			cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
				Name: "vg1",
				StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
					ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimRetain),
				},
			})

			exists, err := r.retainPVsExistForCluster(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("should return false when Retain policy but no PVs", func(ctx context.Context) {
			scName := resource.GetStorageClassName("vg1")
			r := newGinkgoReconciler()
			r.Client = newFieldFilteringClient(r.Client, scName, nil, nil, nil, nil)

			cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
				Name: "vg1",
				StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
					ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimRetain),
				},
			})

			exists, err := r.retainPVsExistForCluster(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should propagate list errors", func(ctx context.Context) {
			scName := resource.GetStorageClassName("vg1")
			expectedErr := fmt.Errorf("PV list failed")
			r := newGinkgoReconciler()
			r.Client = newFieldFilteringClient(r.Client, scName, nil, nil, nil, expectedErr)

			cluster := testLVMCluster(lvmv1alpha1.DeviceClass{
				Name: "vg1",
				StorageClassOptions: &lvmv1alpha1.StorageClassOptions{
					ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimRetain),
				},
			})

			_, err := r.retainPVsExistForCluster(ctx, cluster)
			Expect(err).To(HaveOccurred())
		})
	})
})
