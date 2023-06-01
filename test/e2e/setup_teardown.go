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

package e2e

import (
	"context"

	"github.com/onsi/gomega"
)

// beforeTestSuiteSetup is the function called to initialize the test environment.
func beforeTestSuiteSetup(ctx context.Context) {

	if diskInstall {
		debug("Creating disk for e2e tests\n")
		err := diskSetup(ctx)
		gomega.Expect(err).To(gomega.BeNil())
	}

	if lvmOperatorInstall {
		debug("BeforeTestSuite: deploying LVM Operator\n")
		err := deployLVMWithOLM(ctx, lvmCatalogSourceImage, lvmSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil())
	}

	debug("BeforeTestSuite: starting LVM Cluster\n")
	err := startLVMCluster(ctx)
	gomega.Expect(err).To(gomega.BeNil())

	debug("BeforeTestSuite: creating Namespace %s\n", testNamespace)
	err = createNamespace(ctx, testNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	debug("------------------------------\n")

}

// afterTestSuiteCleanup is the function called to tear down the test environment.
func afterTestSuiteCleanup(ctx context.Context) {

	debug("\n------------------------------\n")

	debug("AfterTestSuite: deleting Namespace %s\n", testNamespace)
	err := deleteNamespaceAndWait(ctx, testNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	if lvmOperatorUninstall {
		debug("AfterTestSuite: deleting default LVM Cluster\n")
		err := deleteLVMCluster(ctx)
		gomega.Expect(err).To(gomega.BeNil())

		debug("AfterTestSuite: uninstalling LVM Operator\n")
		err = uninstallLVM(ctx, lvmCatalogSourceImage, lvmSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil(), "error uninstalling the LVM Operator: %v", err)
	}

	if diskInstall {
		debug("Cleaning up disk\n")
		err = diskRemoval(ctx)
		gomega.Expect(err).To(gomega.BeNil())
	}
}
