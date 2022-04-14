package lvm_test

import (
	"flag"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	tests "github.com/red-hat-storage/lvm-operator/e2e"
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

var _ = Describe("Validation test", func() {
	Context("Validate LVMCluster reconciliation", func() {
		It("Should validate LVMCluster reconciliation", func() {
			err := tests.ValidateResources()
			Expect(err).To(BeNil())
		})
	})
})
