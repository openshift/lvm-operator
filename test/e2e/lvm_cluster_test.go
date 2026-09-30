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

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

func lvmClusterTest() {
	var cluster *v1alpha1.LVMCluster
	BeforeEach(func(ctx SpecContext) {
		waitForExistingClusterDeletion(ctx)
		cluster = GetDefaultTestLVMClusterTemplate()
	})
	AfterEach(func(ctx SpecContext) {
		if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
			skipSuiteCleanup.Store(true)
		}
		// Always delete the cluster to prevent stale resources from
		// causing "duplicate LVMClusters" rejections in subsequent tests.
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
			for i := range cluster.Spec.Storage.DeviceClasses {
				cluster.Spec.Storage.DeviceClasses[i].Default = false
			}

			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)
		})
	})

	Describe("CSI Driver", Serial, func() {
		// A cluster upgraded from a release that predates spec.seLinuxMount still has a
		// CSIDriver without it, so the reconciler has to converge the live object instead of
		// only setting the field on the one it creates. Driving that through a real apiserver
		// also proves the update is accepted: the fake client used in the unit test does not
		// enforce CSIDriver update validation.
		It("should restore seLinuxMount on a driver left over from an older release", func(ctx SpecContext) {
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)
			validateCSIDriverSELinuxMount(ctx)

			By("clearing seLinuxMount the way an older release would have left it")
			driver := &storagev1.CSIDriver{}
			Expect(crClient.Get(ctx, types.NamespacedName{Name: csiDriverName}, driver)).To(Succeed())
			patch := client.MergeFrom(driver.DeepCopy())
			driver.Spec.SELinuxMount = ptr.To(false)
			Expect(crClient.Patch(ctx, driver, patch)).To(Succeed())

			// the LVMCluster reconciler requeues every minute, so nothing needs poking
			validateCSIDriverSELinuxMount(ctx)
		})
	})

	Describe("Thick Provisioning", Serial, func() {
		It("should become ready if ThinPoolConfig is empty (thick provisioning)", func(ctx SpecContext) {
			for i := range cluster.Spec.Storage.DeviceClasses {
				cluster.Spec.Storage.DeviceClasses[i].ThinPoolConfig = nil
			}
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)
		})
	})
}
