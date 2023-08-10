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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
)

// beforeTestSuiteSetup is the function called to initialize the test environment.
func beforeTestSuiteSetup(ctx context.Context) {

	if diskInstall {
		By("Creating disk for e2e tests")
		err := diskSetup(ctx)
		Expect(err).To(BeNil())
	}

	if lvmOperatorInstall {
		By("BeforeTestSuite: deploying LVM Operator")
		err := deployLVMWithOLM(ctx, lvmCatalogSourceImage, lvmSubscriptionChannel)
		Expect(err).To(BeNil())
	}
}

func lvmClusterSetup(clusterConfig *v1alpha1.LVMCluster, ctx context.Context) {
	By("Starting LVM Cluster")
	err := startLVMCluster(clusterConfig, ctx)
	Expect(err).To(BeNil())
}

func lvmNamespaceSetup(ctx context.Context) {
	By("Creating Namespace " + testNamespace)
	err := createNamespace(ctx, testNamespace)
	Expect(err).To(BeNil())
}

func lvmNamespaceCleanup(ctx context.Context) {
	By("Deleting Namespace " + testNamespace)
	err := deleteNamespaceAndWait(ctx, testNamespace)
	Expect(err).To(BeNil())
}

func lvmClusterCleanup(clusterConfig *v1alpha1.LVMCluster, ctx context.Context) {
	By("Deleting LVM Cluster")
	err := deleteLVMCluster(clusterConfig, ctx)
	Expect(err).To(BeNil())
}

// afterTestSuiteCleanup is the function called to tear down the test environment.
func afterTestSuiteCleanup(ctx context.Context) {

	if lvmOperatorUninstall {
		By("AfterTestSuite: uninstalling LVM Operator")
		err := uninstallLVM(ctx, lvmCatalogSourceImage, lvmSubscriptionChannel)
		Expect(err).To(BeNil(), "error uninstalling the LVM Operator: %v", err)
	}

	if diskInstall {
		By("Cleaning up disk")
		err := diskRemoval(ctx)
		Expect(err).To(BeNil())
	}
}
