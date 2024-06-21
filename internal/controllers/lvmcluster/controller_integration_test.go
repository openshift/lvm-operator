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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	secv1 "github.com/openshift/api/security/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
		HaveField("BlockOwnerDeletion", ptr.To(true)),
		HaveField("Controller", ptr.To(true)),
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
	csiDriverName := types.NamespacedName{Name: constants.TopolvmCSIDriverName}
	csiDriverOut := &storagev1.CSIDriver{}

	// VgManager Resource
	vgManagerDaemonset := &appsv1.DaemonSet{}
	vgManagerNamespacedName := types.NamespacedName{Name: resource.VGManagerUnit, Namespace: testLvmClusterNamespace}

	lvmVolumeGroupName := types.NamespacedName{Namespace: testLvmClusterNamespace, Name: testDeviceClassName}
	lvmVolumeGroupOut := &lvmv1alpha1.LVMVolumeGroup{}

	// Topolvm Storage Classes
	var scNames []types.NamespacedName
	for _, deviceClass := range lvmClusterIn.Spec.Storage.DeviceClasses {
		scNames = append(scNames, types.NamespacedName{
			Name: resource.GetStorageClassName(deviceClass.Name),
		})
	}
	scOut := &storagev1.StorageClass{}

	Context("Reconciliation on creating an LVMCluster CR", func() {
		SetDefaultEventuallyTimeout(timeout)
		SetDefaultEventuallyPollingInterval(interval)

		It("should reconcile LVMCluster CR creation", func(ctx context.Context) {
			By("verifying CR status on reconciliation")
			// create node as it should be present
			Expect(k8sClient.Create(ctx, nodeIn)).Should(Succeed())
			// This update is necessary as all nodes get NoSchedule Taint on Creation,
			// and we cannot create it explicitly without taints
			nodeIn.Spec.Taints = make([]corev1.Taint, 0)
			Expect(k8sClient.Update(ctx, nodeIn)).Should(Succeed())

			Expect(k8sClient.Create(ctx, lvmClusterIn)).Should(Succeed())

			By("verifying LVMCluster .Status.Ready is true")
			Eventually(func(ctx context.Context) bool {
				if err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut); err != nil {
					return false
				}
				return lvmClusterOut.Status.Ready
			}).WithContext(ctx).Should(BeTrue(), func() string {
				return fmt.Sprintf("LVMCluster .Status is not ready: %v", lvmClusterOut.Status)
			})

			By("verifying LVMCluster .Status.State is Ready")
			Eventually(func(ctx context.Context) bool {
				if err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut); err != nil {
					return false
				}
				return lvmClusterOut.Status.State == lvmv1alpha1.LVMStatusReady
			}).WithContext(ctx).WithTimeout(10 * time.Second).Should(BeTrue())

			By("verifying LVMCluster .Status.Conditions are Ready")
			Eventually(func(ctx context.Context) bool {
				if err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut); err != nil {
					return false
				}
				for _, c := range lvmClusterOut.Status.Conditions {
					if c.Status == metav1.ConditionFalse {
						return false
					}
				}
				return true
			}).WithContext(ctx).Should(BeTrue())

			By("confirming presence of CSIDriver")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, csiDriverName, csiDriverOut)
			}).WithContext(ctx).Should(Succeed())

			By("confirming presence of vg-manager")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)
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

			By("confirming creation of the SecurityContextConstraints")
			// we only have one SCC for vg-manager
			scc := &secv1.SecurityContextConstraints{}
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: constants.SCCPrefix + "vgmanager"}, scc)
			}).WithContext(ctx).Should(Succeed())
			Expect(scc.Users).ToNot(BeEmpty())
			Expect(scc.Users).To(ContainElement(
				fmt.Sprintf("system:serviceaccount:%s:%s", testLvmClusterNamespace, constants.VGManagerServiceAccount)))
			scc = nil

			By("confirming overwriting the SCC User gets reset")
			Eventually(func(ctx context.Context) []string {
				oldSCC := &secv1.SecurityContextConstraints{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: constants.SCCPrefix + "vgmanager"}, oldSCC)).To(Succeed())
				Expect(k8sClient.Patch(ctx, oldSCC, client.RawPatch(types.MergePatchType, []byte(`{"users": []}`)))).To(Succeed())
				return oldSCC.Users
			}).WithContext(ctx).Should(BeEmpty())

			Eventually(func(ctx context.Context) []string {
				scc := &secv1.SecurityContextConstraints{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: constants.SCCPrefix + "vgmanager"}, scc)).To(Succeed())
				return scc.Users
			}).WithContext(ctx).WithTimeout(5 * time.Second).Should(Not(BeEmpty()))
		})
	})

	Context("Reconciliation on deleting the LVMCluster CR", func() {
		It("should reconcile LVMCluster CR deletion", func(ctx context.Context) {
			By("confirming absence of lvm cluster CR and deletion of operator created resources")
			// deletion of LVMCluster CR
			By("deleting the LVMClusterCR")
			Expect(k8sClient.Delete(ctx, lvmClusterOut)).Should(Succeed())

			// envtest does not support garbage collection, so we simulate the deletion
			// see https://book.kubebuilder.io/reference/envtest.html?highlight=considerations#testing-considerations
			By("confirming vg-manager has owner reference of LVMCluster")
			Expect(k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)).Should(Succeed())
			Expect(vgManagerDaemonset.OwnerReferences).To(containLVMClusterOwnerRefField)
			Expect(k8sClient.Delete(ctx, vgManagerDaemonset)).To(Succeed(), "simulated ownerref cleanup should succeed")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, vgManagerNamespacedName, vgManagerDaemonset)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			By("confirming absence of LVMVolumeGroup Resource")
			// technically we also set ownerrefs on volume groups so we would also need to check,
			// but our controller still deletes them (in addition to the ownerrefs)
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, lvmVolumeGroupName, lvmVolumeGroupOut)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			By("confirming absence of LVMVolumeGroupNodeStatus Resource")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(lvmVolumeGroupNodeStatusIn), lvmVolumeGroupNodeStatusIn)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))

			// auto deletion of CSI Driver resource based on CR deletion
			By("confirming absence of CSI Driver Resource")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, csiDriverName, csiDriverOut)
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

			// create lvmVolumeGroupNodeStatus again to test the removal by the node controller
			lvmVolumeGroupNodeStatusIn.ResourceVersion = ""
			Expect(k8sClient.Create(ctx, lvmVolumeGroupNodeStatusIn)).Should(Succeed())
			By("verifying NodeStatus is created again")
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(lvmVolumeGroupNodeStatusIn), lvmVolumeGroupNodeStatusIn)
			}).WithContext(ctx).Should(Succeed())
			// we will now remove the node which will trigger deletion of the NodeStatus through the node removal controller
			Expect(k8sClient.Delete(ctx, nodeIn)).Should(Succeed())
			Eventually(func(ctx context.Context) error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(lvmVolumeGroupNodeStatusIn), lvmVolumeGroupNodeStatusIn)
			}).WithContext(ctx).Should(Satisfy(errors.IsNotFound))
		})
	})

})
