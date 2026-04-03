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
	"sort"

	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/clusterstatus"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

func deviceRemovalTest() {
	var cluster *v1alpha1.LVMCluster
	BeforeEach(func(ctx SpecContext) {
		infraStatus, err := clusterstatus.GetClusterInfraStatus(ctx, config)
		Expect(err).NotTo(HaveOccurred(), "failed to get cluster infrastructure status")

		if infraStatus.ControlPlaneTopology == configv1.ExternalTopologyMode {
			Skip("Device removal tests are not supported on HyperShift clusters")
		}
		if infraStatus.InfrastructureTopology != configv1.SingleReplicaTopologyMode {
			Skip("Device removal tests run only on single-node (SNO) clusters")
		}

		waitForExistingClusterDeletion(ctx)
		cluster = GetDefaultTestLVMClusterTemplate()
	})
	AfterEach(func(ctx SpecContext) {
		if cluster == nil {
			return
		}
		if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
			skipSuiteCleanup.Store(true)
		}
		DeleteResource(ctx, cluster)
		validateCSINodeInfo(ctx, cluster, false)
	})

	It("should remove devices from volume group successfully", func(ctx SpecContext) {
		By("Creating cluster without device selector to discover available devices")
		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)

		devices := getDiscoveredDevices(ctx, cluster)
		Expect(len(devices)).To(BeNumerically(">=", 2), "at least 2 devices are required for device removal test")
		GinkgoLogr.Info("Discovered devices", "devices", devices)

		smallerDevice, largerDevice := classifyDevicesBySize(devices)
		GinkgoLogr.Info("Classified devices", "smaller", smallerDevice, "larger", largerDevice)

		By("Deleting cluster to recreate with explicit device paths")
		DeleteResource(ctx, cluster)
		validateCSINodeInfo(ctx, cluster, false)
		waitForExistingClusterDeletion(ctx)

		By("Recreating cluster with explicit device paths and thick provisioning")
		cluster = GetDefaultTestLVMClusterTemplate()
		cluster.Spec.Storage.DeviceClasses[0].ThinPoolConfig = nil
		cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
			Paths: []v1alpha1.DevicePath{
				v1alpha1.DevicePath(smallerDevice),
				v1alpha1.DevicePath(largerDevice),
			},
		}
		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)

		By("Removing the smaller device from the volume group")
		Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []v1alpha1.DevicePath{
			v1alpha1.DevicePath(largerDevice),
		}
		Expect(crClient.Update(ctx, cluster)).To(Succeed())

		By("Verifying device removal completed successfully")
		Eventually(func(ctx SpecContext) bool {
			return validateDeviceRemovalSuccess(ctx, cluster, 1)
		}, 5*timeout, interval).WithContext(ctx).Should(BeTrue())
	})

	It("should handle optional device removal", func(ctx SpecContext) {
		By("Creating cluster without device selector to discover available devices")
		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)

		devices := getDiscoveredDevices(ctx, cluster)
		Expect(len(devices)).To(BeNumerically(">=", 2), "at least 2 devices are required for device removal test")
		GinkgoLogr.Info("Discovered devices", "devices", devices)

		smallerDevice, largerDevice := classifyDevicesBySize(devices)
		GinkgoLogr.Info("Classified devices", "smaller", smallerDevice, "larger", largerDevice)

		By("Deleting cluster to recreate with explicit device paths")
		DeleteResource(ctx, cluster)
		validateCSINodeInfo(ctx, cluster, false)
		waitForExistingClusterDeletion(ctx)

		By("Recreating cluster with required and optional devices using thick provisioning")
		cluster = GetDefaultTestLVMClusterTemplate()
		cluster.Spec.Storage.DeviceClasses[0].ThinPoolConfig = nil
		cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
			Paths:         []v1alpha1.DevicePath{v1alpha1.DevicePath(largerDevice)},
			OptionalPaths: []v1alpha1.DevicePath{v1alpha1.DevicePath(smallerDevice)},
		}
		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)

		By("Removing optional devices")
		Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.OptionalPaths = []v1alpha1.DevicePath{}
		Expect(crClient.Update(ctx, cluster)).To(Succeed(), "Should allow removal of optional devices")

		By("Verifying cluster remains Ready with required device only")
		validateLVMCluster(ctx, cluster)
	})
}

// getDiscoveredDevices retrieves the device paths from the LVMCluster status after auto-discovery.
func getDiscoveredDevices(ctx context.Context, cluster *v1alpha1.LVMCluster) []string {
	vgStatus := getVGStatusForCluster(ctx, cluster)
	Expect(vgStatus.Devices).NotTo(BeEmpty(), "cluster should have discovered devices")
	return vgStatus.Devices
}

// classifyDevicesBySize returns (smaller, larger) device by alphabetical order.
// The disk setup creates a 10GB disk before a 30GB disk, and AWS attaches them
// in order, so the first alphabetically is expected to be the smaller one.
func classifyDevicesBySize(devices []string) (smaller, larger string) {
	sorted := make([]string, len(devices))
	copy(sorted, devices)
	sort.Strings(sorted)
	return sorted[0], sorted[len(sorted)-1]
}
