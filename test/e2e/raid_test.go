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

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/clusterstatus"
	k8sv1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

func raidTest() {
	var cluster *v1alpha1.LVMCluster

	BeforeAll(func(ctx SpecContext) {
		infraStatus, err := clusterstatus.GetClusterInfraStatus(ctx, config)
		Expect(err).NotTo(HaveOccurred(), "failed to get cluster infrastructure status")

		if infraStatus.ControlPlaneTopology == configv1.ExternalTopologyMode {
			Skip("RAID tests are not supported on HyperShift clusters")
		}

		waitForExistingClusterDeletion(ctx)

		By("Creating cluster without device selector to discover available devices")
		cluster = GetDefaultTestLVMClusterTemplate()
		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)

		devices := getDiscoveredDevices(ctx, cluster)
		Expect(len(devices)).To(BeNumerically(">=", 2), "at least 2 devices are required for RAID1")
		GinkgoLogr.Info("Discovered devices for RAID", "devices", devices)

		By("Deleting cluster to recreate with RAID config")
		DeleteResource(ctx, cluster)
		validateCSINodeInfo(ctx, cluster, false)
		waitForExistingClusterDeletion(ctx)

		By("Recreating cluster with RAID1 config and optional device paths")
		cluster = GetDefaultTestLVMClusterTemplate()
		cluster.Spec.Storage.DeviceClasses[0].ThinPoolConfig = nil
		cluster.Spec.Storage.DeviceClasses[0].RAIDConfig = &v1alpha1.RAIDConfig{
			Type: v1alpha1.RAIDTypeRAID1,
		}
		optionalPaths := make([]v1alpha1.DevicePath, len(devices))
		for i, d := range devices {
			optionalPaths[i] = v1alpha1.DevicePath(d)
		}
		cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
			OptionalPaths: optionalPaths,
		}
		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)

		DeferCleanup(func(ctx SpecContext) {
			if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
				skipSuiteCleanup.Store(true)
			}
			DeleteResource(ctx, cluster)
			validateCSINodeInfo(ctx, cluster, false)
		})
	})

	It("should report healthy RAID status", func(ctx SpecContext) {
		Eventually(func(ctx SpecContext) error {
			vgStatus := getVGStatusForCluster(ctx, cluster)
			if vgStatus.RAIDStatus == nil {
				return fmt.Errorf("RAID status not yet reported")
			}
			if vgStatus.RAIDStatus.Status != v1alpha1.RAIDHealthStatusHealthy {
				return fmt.Errorf("RAID status is %s, expected Healthy", vgStatus.RAIDStatus.Status)
			}
			if vgStatus.RAIDStatus.MemberCount < 2 {
				return fmt.Errorf("RAID member count is %d, expected at least 2", vgStatus.RAIDStatus.MemberCount)
			}
			if vgStatus.RAIDStatus.DegradedMemberCount != 0 {
				return fmt.Errorf("RAID has %d degraded members, expected 0", vgStatus.RAIDStatus.DegradedMemberCount)
			}
			return nil
		}, timeout, interval).WithContext(ctx).Should(Succeed())
	})

	It("should not create a VolumeSnapshotClass", func(ctx SpecContext) {
		By("Verifying no VolumeSnapshotClass exists for the RAID device class")
		err := crClient.Get(ctx, types.NamespacedName{Name: volumeSnapshotClassName}, &snapapi.VolumeSnapshotClass{})
		if meta.IsNoMatchError(err) {
			GinkgoLogr.Info("VolumeSnapshotClasses are not supported in this cluster, skipping check")
			return
		}
		Expect(err).To(SatisfyAny(
			Satisfy(k8serrors.IsNotFound),
			Satisfy(meta.IsNoMatchError),
		), "VolumeSnapshotClass should not exist for RAID device class")
	})

	It("should provision a filesystem PVC and run a pod", func(ctx SpecContext) {
		pvc := generatePVC(k8sv1.PersistentVolumeFilesystem)
		pod := generatePodConsumingPVC(pvc)

		DeferCleanup(DeleteResources([][]client.Object{{pod, pvc}}))

		CreateResource(ctx, pvc)
		CreateResource(ctx, pod)
		validatePodIsRunning(ctx, client.ObjectKeyFromObject(pod))
		validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))

		expectedData := "RAID-TEST-DATA"
		Expect(contentTester.WriteDataInPod(ctx, pod, expectedData, ContentModeFile)).To(Succeed())
		validatePodData(ctx, pod, expectedData, ContentModeFile)
	})

	It("should provision a block PVC and run a pod", func(ctx SpecContext) {
		pvc := generatePVC(k8sv1.PersistentVolumeBlock)
		pod := generatePodConsumingPVC(pvc)

		DeferCleanup(DeleteResources([][]client.Object{{pod, pvc}}))

		CreateResource(ctx, pvc)
		CreateResource(ctx, pod)
		validatePodIsRunning(ctx, client.ObjectKeyFromObject(pod))
		validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))

		expectedData := "RAID-BLOCK-DATA"
		Expect(contentTester.WriteDataInPod(ctx, pod, expectedData, ContentModeBlock)).To(Succeed())
		validatePodData(ctx, pod, expectedData, ContentModeBlock)
	})

	It("should expand a PVC", func(ctx SpecContext) {
		pvc := generatePVC(k8sv1.PersistentVolumeFilesystem)
		pvc.SetName("raid-expand")
		pod := generatePodConsumingPVC(pvc)

		DeferCleanup(DeleteResources([][]client.Object{{pod, pvc}}))

		CreateResource(ctx, pvc)
		CreateResource(ctx, pod)
		validatePodIsRunning(ctx, client.ObjectKeyFromObject(pod))
		validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))

		By("Expanding the PVC from 1Gi to 2Gi")
		Expect(crClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc)).To(Succeed())
		pvc.Spec.Resources.Requests[k8sv1.ResourceStorage] = resource.MustParse("2Gi")
		Expect(crClient.Update(ctx, pvc)).To(Succeed())

		By("Verifying PVC capacity increased")
		Eventually(func(ctx SpecContext) bool {
			if err := crClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil {
				return false
			}
			capacity := pvc.Status.Capacity[k8sv1.ResourceStorage]
			return capacity.Cmp(resource.MustParse("2Gi")) >= 0
		}, timeout, interval).WithContext(ctx).Should(BeTrue(), "PVC capacity should reach 2Gi")
	})
}
