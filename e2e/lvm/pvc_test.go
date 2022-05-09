package lvm_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tests "github.com/red-hat-storage/lvm-operator/e2e"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	timeout  = time.Minute * 5
	interval = time.Second * 30
)

// Test to verify the PVC status
func PVCTest() {

	Describe("PVC Tests", func() {
		var filepvc *k8sv1.PersistentVolumeClaim
		var filepod *k8sv1.Pod
		var blockpvc *k8sv1.PersistentVolumeClaim
		var blockpod *k8sv1.Pod

		Context("create pvc and pod", func() {
			It("Verifies the status", func() {
				By("Creates pvc and pod")
				filepvc = tests.GetSamplePVC(tests.StorageClass, "5Gi", "lvmfilepvc", k8sv1.PersistentVolumeFilesystem)
				filepod = tests.GetSamplePod("lvmfilepod", "lvmfilepvc")
				err := tests.DeployManagerObj.GetCrClient().Create(context.TODO(), filepvc)
				Expect(err).To(BeNil())
				err = tests.DeployManagerObj.GetCrClient().Create(context.TODO(), filepod)
				Expect(err).To(BeNil())

				blockpvc = tests.GetSamplePVC(tests.StorageClass, "5Gi", "lvmblockpvc", k8sv1.PersistentVolumeBlock)
				blockpod = tests.GetSamplePod("lvmblockpod", "lvmblockpvc")
				err = tests.DeployManagerObj.GetCrClient().Create(context.TODO(), blockpvc)
				Expect(err).To(BeNil())
				err = tests.DeployManagerObj.GetCrClient().Create(context.TODO(), blockpod)
				Expect(err).To(BeNil())

				By("PVC(file system) Should be bound and Pod should be running")
				Eventually(func() bool {
					err := tests.DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: filepvc.Name, Namespace: filepvc.Namespace}, filepvc)
					return err == nil && filepvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", filepvc.Name)

				Eventually(func() bool {
					err = tests.DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: filepod.Name, Namespace: filepod.Namespace}, filepod)
					return err == nil && filepod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", filepod.Name)

				err = tests.DeployManagerObj.GetCrClient().Delete(context.TODO(), filepod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", filepod.Name)

				err = tests.DeployManagerObj.GetCrClient().Delete(context.TODO(), filepvc)
				Expect(err).To(BeNil())
				fmt.Printf("PVC %s is deleted\n", filepvc.Name)

				By("PVC(block) Should be bound and Pod should be running")
				Eventually(func() bool {
					err := tests.DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: blockpvc.Name, Namespace: blockpvc.Namespace}, blockpvc)
					return err == nil && blockpvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", blockpvc.Name)

				Eventually(func() bool {
					err = tests.DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: blockpod.Name, Namespace: blockpod.Namespace}, blockpod)
					return err == nil && blockpod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", blockpod.Name)

				err = tests.DeployManagerObj.GetCrClient().Delete(context.TODO(), blockpod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", blockpod.Name)

				err = tests.DeployManagerObj.GetCrClient().Delete(context.TODO(), blockpvc)
				Expect(err).To(BeNil())
				fmt.Printf("PVC %s is deleted\n", blockpvc.Name)
			})
		})

	})
}
