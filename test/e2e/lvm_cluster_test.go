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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
		BeforeEach(func(ctx SpecContext) {
			if !IsSNO(ctx) {
				Skip("Device removal tests run only on SNO instances")
			}
		})

		It("should remove devices from volume group successfully", func(ctx SpecContext) {
			// Configure cluster with multiple devices for removal testing
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
				Paths: []v1alpha1.DevicePath{"/dev/nvme3n1", "/dev/nvme4n1"},
			}

			By("Creating cluster with multiple devices")
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			By("Executing pvmove to migrate data from device to be removed")
			err := executePvmoveOnVGManagerPods(ctx, "/dev/nvme3n1", "/dev/nvme4n1")
			Expect(err).NotTo(HaveOccurred(), "pvmove should succeed before device removal")

			By("Removing one device from the volume group")
			// Update cluster to remove /dev/nvme1n1
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []v1alpha1.DevicePath{
				"/dev/nvme4n1",
			}

			err = crClient.Update(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying device removal completed successfully")
			Eventually(func(ctx SpecContext) bool {
				return validateDeviceRemovalSuccess(ctx, cluster, 1)
			}, 5*timeout, interval).WithContext(ctx).Should(BeTrue())
		})

		It("should handle optional device removal", func(ctx SpecContext) {
			// Configure cluster with both required and optional devices
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
				Paths:         []v1alpha1.DevicePath{"/dev/nvme4n1"},
				OptionalPaths: []v1alpha1.DevicePath{"/dev/nvme1n1", "/dev/nvme3n1"},
			}

			By("Creating cluster with required and optional devices")
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			By("Executing pvmove to migrate data from optional devices before removal")
			err := executePvmoveOnVGManagerPods(ctx, "/dev/nvme3n1", "/dev/nvme4n1")
			Expect(err).NotTo(HaveOccurred(), "pvmove should succeed for optional device")

			By("Removing optional devices")
			cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.OptionalPaths = []v1alpha1.DevicePath{}

			err = crClient.Update(ctx, cluster)
			Expect(err).NotTo(HaveOccurred(), "Should allow removal of optional devices")

			By("Verifying cluster remains Ready with required device only")
			Eventually(func(ctx SpecContext) bool {
				return validateClusterReady(ctx, cluster)
			}, timeout, interval).WithContext(ctx).Should(BeTrue())
		})
	})
}

// executePvmoveOnVGManagerPods executes pvmove command on all vg-manager pods
func executePvmoveOnVGManagerPods(ctx context.Context, sourceDevice, targetDevice string) error {
	// Get all vg-manager pods
	podList := &k8sv1.PodList{}
	err := crClient.List(ctx, podList, &client.ListOptions{
		Namespace: installNamespace,
		LabelSelector: labels.Set{
			"app.kubernetes.io/name": "vg-manager",
		}.AsSelector(),
	})
	if err != nil {
		return fmt.Errorf("failed to list vg-manager pods: %w", err)
	}

	// Create pod runner for command execution
	podRunner, err := NewPodRunner(config, scheme)
	if err != nil {
		return fmt.Errorf("failed to create pod runner: %w", err)
	}

	// Execute pvmove on each vg-manager pod that has the device
	for _, pod := range podList.Items {
		// Execute pvmove to migrate data away from the source device
		pvmoveCmd := fmt.Sprintf("nsenter -m -u -i -n -p -t 1 pvmove %s %s", sourceDevice, targetDevice)
		stdout, stderr, err := podRunner.ExecCommandInFirstPodContainer(ctx, &pod, pvmoveCmd)
		if err != nil {
			return fmt.Errorf("pvmove failed on pod %s (node: %s): %w\nstdout: %s\nstderr: %s",
				pod.Name, pod.Spec.NodeName, err, stdout, stderr)
		}
	}

	return nil
}
