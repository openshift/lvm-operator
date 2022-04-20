package lvm_test

import (
	"context"
	"flag"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tests "github.com/red-hat-storage/lvm-operator/e2e"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLvm(t *testing.T) {
	flag.Parse()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lvm Suite")
}

var _ = BeforeSuite(func() {
	tests.BeforeTestSuiteSetup()
})

var _ = AfterSuite(func() {
	tests.AfterTestSuiteCleanup()
})

var _ = Describe("lvmtest", func() {
	Context("Run a dummy test", func() {
		It("Should do nothing", func() {
			fmt.Println("Do nothing")
		})
	})
})

// Test to verify the PVC status
var _ = Describe("PVC Status check", VerifyPVCStatus)

func VerifyPVCStatus() {

	Describe("pvc", func() {
		var pvc *k8sv1.PersistentVolumeClaim
		var pod *k8sv1.Pod
		var namespace string

		BeforeEach(func() {
			namespace = tests.TestNamespace
			pvc = tests.GetSamplePVC(tests.StorageClass, "5Gi")
			pod = tests.GetSamplePod()
		})

		AfterEach(func() {
			err := tests.DeployManagerObj.GetK8sClient().CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).To(BeNil())
			}
			err = tests.DeployManagerObj.GetK8sClient().CoreV1().Pods(namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).To(BeNil())
			}
		})

		Context("create pvc and pod", func() {
			It("and verify bound status", func() {
				By("Should be bound")
				err := tests.WaitForPVCBound(pvc, namespace, pod)
				fmt.Printf("%v", err)
				Expect(err).To(BeNil())

			})
		})

	})
}
