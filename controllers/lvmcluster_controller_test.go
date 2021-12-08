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
			By("Indicate setting status to ready")
			ctx := context.Background()
			lvmCluster := &lvmv1alpha1.LVMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testLvmClusterName,
					Namespace: testLvmClusterNamespace,
				},
				Spec: lvmv1alpha1.LVMClusterSpec{
					DeviceClasses: []lvmv1alpha1.DeviceClass{{Name: "test"}},
				},
			}
			Expect(k8sClient.Create(ctx, lvmCluster)).Should(Succeed())

			//Check that the status.Ready field has been set to true. This is a placeholder test and will
			// be modified to check for the actual resources once they are implemented.

			lvmClusterLookupName := types.NamespacedName{Name: testLvmClusterName, Namespace: testLvmClusterNamespace}
			lvmCluster1 := &lvmv1alpha1.LVMCluster{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lvmClusterLookupName, lvmCluster1)
				if err != nil {
					return false
				}
				return lvmCluster1.Status.Ready
			}, timeout, interval).Should(BeTrue())
			// Let's make sure our Schedule string value was properly converted/handled.
			Expect(lvmCluster1.Status.Ready).Should(Equal(true))
		})

	})

})
