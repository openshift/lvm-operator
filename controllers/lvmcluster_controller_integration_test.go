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
)

var _ = Describe("LVMCluster controller", func() {

	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	ctx := context.Background()

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
	scNames := []types.NamespacedName{}
	for _, deviceClass := range lvmClusterIn.Spec.Storage.DeviceClasses {
		scNames = append(scNames, types.NamespacedName{
			Name: getStorageClassName(deviceClass.Name),
		},
		)
	}
	scOut := &storagev1.StorageClass{}

	Context("Reconciliation on creating an LVMCluster CR", func() {
		It("should reconcile LVMCluster CR creation, ", func() {
			By("verifying CR status on reconciliation")
			Expect(k8sClient.Create(ctx, lvmClusterIn)).Should(Succeed())

			// create node as it should be present
			Expect(k8sClient.Create(ctx, nodeIn)).Should(Succeed())
			// create lvmVolumeGroupNodeStatus as it should be created by vgmanager and
			// lvmcluster controller expecting this to be present to set the status properly
			Expect(k8sClient.Create(ctx, lvmVolumeGroupNodeStatusIn)).Should(Succeed())

			// placeholder to check CR status.Ready field to be true
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut)
				if err != nil {
					return false
				}
				return lvmClusterOut.Status.Ready
			}, timeout, interval).Should(Equal(true))

			// placeholder to check CR status.State field to be Ready
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut)
				if err != nil {
					return false
				}
				return lvmClusterOut.Status.State == lvmv1alpha1.LVMStatusReady
			}, timeout, interval).Should(BeTrue())

			By("confirming presence of CSIDriver resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, csiDriverName, csiDriverOut)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("confirming presence of VgManager resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("confirming presence of Topolvm Controller deployment")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, controllerName, controllerOut)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("confirming the existence of CSI Node resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, csiNodeName, csiNodeOut)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("confirming the existence of LVMVolumeGroup resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmVolumeGroupName, lvmVolumeGroupOut)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("confirming creation of TopolvmStorageClasses")
			for _, scName := range scNames {
				Eventually(func() bool {
					err := k8sClient.Get(ctx, scName, scOut)
					return err == nil
				}, timeout, interval).Should(BeTrue())
				scOut = &storagev1.StorageClass{}
			}
		})
	})

	Context("Reconciliation on deleting the LVMCluster CR", func() {
		It("should reconcile LVMCluster CR deletion ", func() {
			By("confirming absence of lvm cluster CR and deletion of operator created resources")

			// delete lvmVolumeGroupNodeStatus as it should be deleted by vgmanager
			// and if it is present lvmcluster reconciler takes it as vg is present on node
			Expect(k8sClient.Delete(ctx, lvmVolumeGroupNodeStatusIn)).Should(Succeed())

			// deletion of LVMCluster CR
			Eventually(func() bool {
				err := k8sClient.Delete(ctx, lvmClusterOut)
				return err != nil
			}, timeout, interval).Should(BeTrue())

			// auto deletion of CSI Driver resource based on CR deletion
			By("confirming absence of CSI Driver Resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, csiDriverName, csiDriverOut)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			// ensure that VgManager has owner reference of LVMCluster. (envTest does not support garbage collection)
			By("confirming VgManager resource has owner reference of LVMCluster resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)
				return err == nil && vgManagerDaemonset.OwnerReferences[0].Name == lvmClusterIn.Name
			}, timeout, interval).Should(BeTrue())

			// auto deletion of Topolvm Controller deployment based on CR deletion
			By("confirming absence of Topolvm Controller Deployment")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, controllerName, controllerOut)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("confirming absence of CSI Node Resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, csiNodeName, csiNodeOut)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("confirming absence of LVMVolumeGroup Resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmVolumeGroupName, lvmVolumeGroupOut)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("confirming absence of TopolvmStorageClasses")
			for _, scName := range scNames {
				Eventually(func() bool {
					err := k8sClient.Get(ctx, scName, scOut)
					return errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
				scOut = &storagev1.StorageClass{}
			}

			By("confirming absence of LVMCluster CR")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

		})
	})

})
