package e2e

import "github.com/onsi/gomega"

// BeforeTestSuiteSetup is the function called to initialize the test environment.
func BeforeTestSuiteSetup() {

	SuiteFailed = true
	if lvmOperatorInstall {
		debug("BeforeTestSuite: deploying LVM Operator\n")
		err := DeployManagerObj.DeployLVMWithOLM(LvmCatalogSourceImage, LvmSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil())
	}

	debug("BeforeTestSuite: starting LVM Cluster\n")
	err := DeployManagerObj.StartLVMCluster()
	gomega.Expect(err).To(gomega.BeNil())

	debug("BeforeTestSuite: creating Namespace %s\n", TestNamespace)
	err = DeployManagerObj.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	SuiteFailed = false

	debug("------------------------------\n")

}

// AfterTestSuiteCleanup is the function called to tear down the test environment.
func AfterTestSuiteCleanup() {

	debug("\n------------------------------\n")

	debug("AfterTestSuite: deleting Namespace %s\n", TestNamespace)
	err := DeployManagerObj.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	if lvmOperatorUninstall {
		debug("AfterTestSuite: deleting default LVM CLuster\n")
		err := DeployManagerObj.DeleteLVMCluster()
		gomega.Expect(err).To(gomega.BeNil())

		debug("AfterTestSuite: uninstalling LVM Operator\n")
		err = DeployManagerObj.UninstallLVM(LvmCatalogSourceImage, LvmSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil(), "error uninstalling LVM: %v", err)
	}
}
