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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

func lvmClusterTest() {
	var cluster *v1alpha1.LVMCluster
	BeforeEach(func(ctx SpecContext) {
		cluster = GetDefaultTestLVMClusterTemplate()
	})
	AfterEach(func(ctx SpecContext) {
		if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
			By("Test failed, skipping cluster cleanup")
			skipSuiteCleanup.Store(true)
			return
		}
		DeleteResource(ctx, cluster)
		validateCSINodeInfo(ctx, cluster, false)
	})

	Describe("Filesystem Type", Serial, func() {
		It("should default to xfs", func(ctx SpecContext) {
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			By("Verifying that the default FS type is set to XFS on the StorageClass")
			sc := GetStorageClass(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace})
			Expect(sc.Parameters["csi.storage.k8s.io/fstype"]).To(Equal(string(v1alpha1.FilesystemTypeXFS)))
		})

		DescribeTable("fstype", func(ctx SpecContext, fsType v1alpha1.DeviceFilesystemType) {
			By(fmt.Sprintf("modifying cluster template to have file system %s by default", fsType))
			cluster.Spec.Storage.DeviceClasses[0].FilesystemType = fsType

			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			By("Verifying the correct fstype Parameter")
			sc := GetStorageClass(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace})
			Expect(sc.Parameters["csi.storage.k8s.io/fstype"]).To(Equal(string(fsType)))
		},
			Entry("xfs", v1alpha1.FilesystemTypeXFS),
			Entry("ext4", v1alpha1.FilesystemTypeExt4),
		)
	})

	Describe("Storage Class", Serial, func() {
		It("should become ready without a default storageclass", func(ctx SpecContext) {
			// set default to false
			for _, dc := range cluster.Spec.Storage.DeviceClasses {
				dc.Default = false
			}

			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)
		})
	})

	Describe("Thick Provisioning", Serial, func() {
		It("should become ready if ThinPoolConfig is empty (thick provisioning)", func(ctx SpecContext) {
			for _, dc := range cluster.Spec.Storage.DeviceClasses {
				dc.ThinPoolConfig = nil
			}
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)
		})
	})

	Describe("Device Removal", Serial, func() {
		It("should remove devices from volume group successfully", func(ctx SpecContext) {
			// Configure cluster with multiple devices for removal testing
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
				Paths: []v1alpha1.DevicePath{"/dev/sdg", "/dev/sdh"},
			}

			By("Creating cluster with multiple devices")
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			By("Removing one device from the volume group")
			// Update cluster to remove /dev/sdi
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []v1alpha1.DevicePath{
				"/dev/sdg",
			}

			err := crClient.Update(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying device removal completed successfully")
			Eventually(func(ctx SpecContext) bool {
				return validateDeviceRemovalSuccess(ctx, cluster, 1)
			}, 5*timeout, interval).WithContext(ctx).Should(BeTrue())
		})

		It("should handle optional device removal", func(ctx SpecContext) {
			// Configure cluster with both required and optional devices
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
				Paths:         []v1alpha1.DevicePath{"/dev/sdh"},
				OptionalPaths: []v1alpha1.DevicePath{"/dev/sdl", "/dev/sdm"},
			}

			By("Creating cluster with required and optional devices")
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			By("Removing optional devices")
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.OptionalPaths = []v1alpha1.DevicePath{}

			err := crClient.Update(ctx, cluster)
			Expect(err).NotTo(HaveOccurred(), "Should allow removal of optional devices")

			By("Verifying cluster remains Ready with required device only")
			Eventually(func(ctx SpecContext) bool {
				return validateClusterReady(ctx, cluster)
			}, timeout, interval).WithContext(ctx).Should(BeTrue())
		})
	})
}
