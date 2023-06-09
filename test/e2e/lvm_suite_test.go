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
	"flag"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/lvm-operator/api/v1alpha1"
)

func TestLvm(t *testing.T) {
	flag.Parse()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lvm Suite")
}

var _ = BeforeSuite(func() {
	// Configure the disk and install the operator
	beforeTestSuiteSetup(context.Background())
	lvmNamespaceSetup(context.Background())
})

var _ = AfterSuite(func() {
	lvmNamespaceCleanup(context.Background())
	afterTestSuiteCleanup(context.Background())
})

var _ = Describe("LVM Operator e2e tests", func() {
	Describe("LVM Cluster Configuration", Serial, lvmClusterTest)

	Describe("LVM Operator", Ordered, func() {
		// Ordered to give the BeforeAll/AfterAll functionality to achieve common setup
		var clusterConfig *v1alpha1.LVMCluster

		BeforeAll(func() {
			clusterConfig = generateLVMCluster()
			lvmClusterSetup(clusterConfig, context.Background())
		})

		Describe("Functional Tests", func() {
			Context("LVMCluster reconciliation", validateResources)
			Context("PVC tests", pvcTest)
			Context("Ephemeral volume tests", ephemeralTest)
		})

		AfterAll(func() {
			lvmClusterCleanup(clusterConfig, context.Background())
		})
	})
})
