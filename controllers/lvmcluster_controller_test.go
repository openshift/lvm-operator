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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
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

	Context("LvmCluster reconcile", func() {
		It("Reconciles an LvmCluster, ", func() {

			ctx := context.Background()

			// LVM CR
			lvmClusterName := types.NamespacedName{Name: testLvmClusterName, Namespace: testLvmClusterNamespace}
			lvmClusterOut := &lvmv1alpha1.LVMCluster{}
			lvmClusterIn := &lvmv1alpha1.LVMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testLvmClusterName,
					Namespace: testLvmClusterNamespace,
				},
				Spec: lvmv1alpha1.LVMClusterSpec{
					DeviceClasses: []lvmv1alpha1.DeviceClass{{Name: "test"}},
				},
			}

			// CSI Driver Resource
			csiDriverName := types.NamespacedName{Name: TopolvmCSIDriverName}
			csiDriverOut := &storagev1.CSIDriver{}

			// Topolvm Controller Deployment
			controllerName := types.NamespacedName{Name: TopolvmControllerDeploymentName, Namespace: testLvmClusterNamespace}
			controllerOut := &appsv1.Deployment{}

			By("Indicate setting CR status to be ready after CR is deployed")
			Expect(k8sClient.Create(ctx, lvmClusterIn)).Should(Succeed())

			// placeholder to check CR status.Ready field to be true
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut)
				if err != nil {
					return false
				}
				return lvmClusterOut.Status.Ready
			}, timeout, interval).Should(Equal(true))

			// presence of csi driver resource
			By("Confirming csi driver resource is present")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, csiDriverName, csiDriverOut)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// presence of topolvm controller deployment
			By("Confirming topolvm deployment is present")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, controllerName, controllerOut)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Confirming deletion of lvm cluster")
			Eventually(func() bool {
				err := k8sClient.Delete(ctx, lvmClusterOut)

				// deletion of CSI Driver resource
				By("Confirming deletion of CSI Driver Resource")
				Eventually(func() bool {
					err := k8sClient.Get(ctx, csiDriverName, csiDriverOut)
					return errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())

				// deletion of Topolvm Controller Deployment
				By("Confirming deletion of Topolvm Controller Deployment")
				Eventually(func() bool {
					err := k8sClient.Get(ctx, controllerName, controllerOut)
					return errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())

				// Deletion of LVM Cluster CR
				return err != nil

			}, timeout, interval).Should(BeTrue())

		})

	})

})
