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

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("LVMCluster controller", func() {

	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	// LVMCluster CR details
	lvmClusterName := types.NamespacedName{Name: testLvmClusterName, Namespace: testLvmClusterNamespace}
	lvmClusterOut := &lvmv1alpha1.LVMCluster{}
	lvmClusterIn := &lvmv1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testLvmClusterName,
			Namespace: testLvmClusterNamespace,
		},
		Spec: lvmv1alpha1.LVMClusterSpec{
			Storage: lvmv1alpha1.Storage{
				DeviceClasses: []lvmv1alpha1.DeviceClass{{
					Name:    testDeviceClassName,
					Default: true,
					ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
						Name:               testThinPoolName,
						SizePercent:        50,
						OverprovisionRatio: 10,
					},
				}},
			},
		},
	}

	// this is a custom matcher that verifies for a correct owner-ref set with LVMCluster
	containLVMClusterOwnerRefField := ContainElement(SatisfyAll(
		HaveField("Name", lvmClusterIn.Name),
		HaveField("BlockOwnerDeletion", pointer.Bool(true)),
		HaveField("Controller", pointer.Bool(true)),
	))

	nodeIn := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNodeName,
		},
	}

	lvmVolumeGroupNodeStatusIn := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNodeName,
			Namespace: testLvmClusterNamespace,
		},
		Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
			LVMVGStatus: []lvmv1alpha1.VGStatus{
				{
					Name:   testDeviceClassName,
					Status: lvmv1alpha1.VGStatusReady,
				},
			},
		},
	}

	// CSI Driver Resource
	csiDriverName := types.NamespacedName{Name: TopolvmCSIDriverName}
	csiDriverOut := &storagev1.CSIDriver{}

	// VgManager Resource
	vgManagerDaemonset := &appsv1.DaemonSet{}
	vgManagerNamespacedName := types.NamespacedName{Name: VGManagerUnit, Namespace: testLvmClusterNamespace}

	// Topolvm Controller Deployment
	controllerName := types.NamespacedName{Name: TopolvmControllerDeploymentName, Namespace: testLvmClusterNamespace}
	controllerOut := &appsv1.Deployment{}

	// CSI Node resource
	csiNodeName := types.NamespacedName{Namespace: testLvmClusterNamespace, Name: TopolvmNodeDaemonsetName}
	csiNodeOut := &appsv1.DaemonSet{}

	lvmVolumeGroupName := types.NamespacedName{Namespace: testLvmClusterNamespace, Name: testDeviceClassName}
	lvmVolumeGroupOut := &lvmv1alpha1.LVMVolumeGroup{}

	// Topolvm Storage Classes
	var scNames []types.NamespacedName
	for _, deviceClass := range lvmClusterIn.Spec.Storage.DeviceClasses {
		scNames = append(scNames, types.NamespacedName{
			Name: getStorageClassName(deviceClass.Name),
		})
	}
	scOut := &storagev1.StorageClass{}

	Context("Reconciliation on creating an LVMCluster CR", func() {
		SetDefaultEventuallyTimeout(timeout)
		SetDefaultEventuallyPollingInterval(interval)

		It("should reconcile LVMCluster CR creation, ", func(ctx context.Context) {
			By("verifying CR status on reconciliation")
			// create node as it should be present
			Expect(k8sClient.Create(ctx, nodeIn)).Should(Succeed())
			// This update is necessary as all nodes get NoSchedule Taint on Creation,
			// and we cannot create it explicitly without taints
			nodeIn.Spec.Taints = make([]corev1.Taint, 0)
			Expect(k8sClient.Update(ctx, nodeIn)).Should(Succeed())

			Expect(k8sClient.Create(ctx, lvmClusterIn)).Should(Succeed())

			// create lvmVolumeGroupNodeStatus as it should be created by vgmanager and
			// lvmcluster controller expecting this to be present to set the status properly
			Expect(k8sClient.Create(ctx, lvmVolumeGroupNodeStatusIn)).Should(Succeed())

			By("verifying LVMCluster .Status.Ready is true")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut); err != nil {
					return false
				}
				return lvmClusterOut.Status.Ready
			}).Should(BeTrue())

			By("verifying LVMCluster .Status.State is Ready")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut); err != nil {
					return false
				}
				return lvmClusterOut.Status.State == lvmv1alpha1.LVMStatusReady
			}).Should(BeTrue())

			By("confirming presence of CSIDriver")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, csiDriverName, csiDriverOut)
			}).WithContext(ctx).Should(Succeed())

			By("confirming presence of vg-manager")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)
			}).WithContext(ctx).Should(Succeed())

			By("confirming presence of TopoLVM Controller Deployment")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, controllerName, controllerOut)
			}).WithContext(ctx).Should(Succeed())

			By("confirming the existence of CSI Node resource")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, csiNodeName, csiNodeOut)
			}).WithContext(ctx).Should(Succeed())

			By("confirming the existence of LVMVolumeGroup resource")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, lvmVolumeGroupName, lvmVolumeGroupOut)
			}).WithContext(ctx).Should(Succeed())

			By("confirming creation of TopolvmStorageClasses")
			for _, scName := range scNames {
				Eventually(func(ctx context.Context) error {
					return k8sClient.Get(ctx, scName, scOut)
				}).WithContext(ctx).Should(Succeed())
				scOut = &storagev1.StorageClass{}
			}
		})
	})

	Context("Reconciliation on deleting the LVMCluster CR", func() {
		It("should reconcile LVMCluster CR deletion ", func(ctx context.Context) {

			// delete lvmVolumeGroupNodeStatus as it should be deleted by vgmanager
			// and if it is present lvmcluster reconciler takes it as vg is present on node
			// we will now remove the node which will cause the LVM cluster status to also lose that vg
			By("confirming absence of lvm cluster CR and deletion of operator created resources")
			Expect(k8sClient.Delete(ctx, nodeIn)).Should(Succeed())
			// deletion of LVMCluster CR and thus also the NodeStatus through the removal controller
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(lvmVolumeGroupNodeStatusIn), lvmVolumeGroupNodeStatusIn)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			// deletion of LVMCluster CR
			By("deleting the LVMClusterCR")
			Expect(k8sClient.Delete(ctx, lvmClusterOut)).Should(Succeed())

			// auto deletion of CSI Driver resource based on CR deletion
			By("confirming absence of CSI Driver Resource")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, csiDriverName, csiDriverOut)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			// envtest does not support garbage collection, so we simulate the deletion
			// see https://book.kubebuilder.io/reference/envtest.html?highlight=considerations#testing-considerations
			By("confirming vg-manager has owner reference of LVMCluster")
			Expect(k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)).Should(Succeed())
			Expect(vgManagerDaemonset.OwnerReferences).To(containLVMClusterOwnerRefField)
			Expect(k8sClient.Delete(ctx, vgManagerDaemonset)).To(Succeed(), "simulated ownerref cleanup should succeed")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			// envtest does not support garbage collection, so we simulate the deletion
			// see https://book.kubebuilder.io/reference/envtest.html?highlight=considerations#testing-considerations
			By("confirming TopoLVM controller resource has owner reference of LVMCluster")
			Expect(k8sClient.Get(ctx, controllerName, controllerOut)).Should(Succeed())
			Expect(controllerOut.OwnerReferences).To(containLVMClusterOwnerRefField)
			Expect(k8sClient.Delete(ctx, controllerOut)).To(Succeed(), "simulated ownerref cleanup should succeed")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, controllerName, controllerOut)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			// envtest does not support garbage collection, so we simulate the deletion
			// see https://book.kubebuilder.io/reference/envtest.html?highlight=considerations#testing-considerations
			By("confirming TopoLVM Node DaemonSet has owner reference of LVMCluster")
			Expect(k8sClient.Get(ctx, csiNodeName, csiNodeOut)).Should(Succeed())
			Expect(csiNodeOut.OwnerReferences).To(containLVMClusterOwnerRefField)
			Expect(k8sClient.Delete(ctx, csiNodeOut)).To(Succeed(), "simulated ownerref cleanup should succeed")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, csiNodeName, csiNodeOut)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			By("confirming absence of LVMVolumeGroup Resource")
			// technically we also set ownerrefs on volume groups so we would also need to check,
			// but our controller still deletes them (in addition to the ownerrefs)
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, lvmVolumeGroupName, lvmVolumeGroupOut)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			By("confirming absence of TopolvmStorageClasses")
			for _, scName := range scNames {
				Eventually(func(ctx context.Context) error {
					return k8sClient.Get(ctx, scName, scOut)
				}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))
			}

			By("confirming absence of LVMCluster CR")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, lvmClusterName, lvmClusterOut)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))
		})
	})

})
