package lvm_test

import (
	"flag"
	"fmt"
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

var _ = Describe("lvmtest", func() {
	Context("Run a dummy test", func() {
		It("Should do nothing", func() {
			fmt.Println("Do nothing")
		})
	})
})
