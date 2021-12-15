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
			DeviceClasses: []lvmv1alpha1.DeviceClass{{Name: "test"}},
		},
	}

	// CSI Driver Resource
	csiDriverName := types.NamespacedName{Name: TopolvmCSIDriverName}
	csiDriverOut := &storagev1.CSIDriver{}

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

			// deletion of CSI Driver resource
			By("confirming absence of CSI Driver Resource")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, csiDriverName, csiDriverOut)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("confirming absence of LVMCluster CR")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmClusterName, lvmClusterOut)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

		})
	})

})
