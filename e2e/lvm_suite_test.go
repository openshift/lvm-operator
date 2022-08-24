package e2e

import (
	"context"
	"flag"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLvm(t *testing.T) {
	flag.Parse()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lvm Suite")
}

var _ = BeforeSuite(func() {
	beforeTestSuiteSetup(context.Background())
})

var _ = AfterSuite(func() {
	afterTestSuiteCleanup(context.Background())
})

var _ = Describe("LVMO e2e tests", func() {
	Context("LVMCluster reconciliation", validateResources)
	Context("PVC tests", pvcTest)
	Context("Ephemeral volume tests", ephemeralTest)
})
