/*
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
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
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
					Name: testDeviceClassName,
					ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
						Name:               testThinPoolName,
						SizePercent:        50,
						OverprovisionRatio: 10,
					},
					NodeSelector: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: hostNameLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"node1"}},
								},
							},
						},
					},
				}},
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
			Name: fmt.Sprintf("odf-lvm-%s", deviceClass.Name),
		},
		)
	}
	scOut := &storagev1.StorageClass{}

	os.Setenv("VGMANAGER_IMAGE", "test")
	Context("Reconciliation on creating an LVMCluster CR", func() {
		It("should reconcile LVMCluster CR creation, ", func() {
			By("verifying CR status.Ready is set to true on reconciliation")
			Expect(k8sClient.Create(ctx, lvmClusterIn)).Should(Succeed())

			// placeholder to check CR status.Ready field to be true
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut)
				if err != nil {
					return false
				}
				return lvmClusterOut.Status.Ready
			}, timeout, interval).Should(Equal(true))

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
