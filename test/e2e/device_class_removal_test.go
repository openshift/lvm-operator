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

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/clusterstatus"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/resource"
)

func deviceClassRemovalTest() {
	Describe("Cleanup", Serial, func() {
		var cluster *v1alpha1.LVMCluster
		BeforeEach(func(ctx SpecContext) {
			infraStatus, err := clusterstatus.GetClusterInfraStatus(ctx, config)
			Expect(err).NotTo(HaveOccurred(), "failed to get cluster infrastructure status")

			if infraStatus.ControlPlaneTopology == configv1.ExternalTopologyMode {
				Skip("Device class removal tests are not supported on HyperShift clusters")
			}
			if infraStatus.InfrastructureTopology != configv1.SingleReplicaTopologyMode {
				Skip("Device class removal tests run only on single-node (SNO) clusters")
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

		It("should remove a non-default device class and clean up its resources", func(ctx SpecContext) {
			cluster = createClusterWithTwoDeviceClasses(ctx, cluster, false)

			vg2SCName := resource.GetStorageClassName("vg2")

			By("Removing the non-default device class (vg2) from the cluster")
			Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			cluster.Spec.Storage.DeviceClasses = filterDeviceClasses(cluster.Spec.Storage.DeviceClasses, "vg2")
			Expect(crClient.Update(ctx, cluster)).To(Succeed())

			By("Verifying vg2 StorageClass is deleted")
			Eventually(func(ctx SpecContext) error {
				return crClient.Get(ctx, types.NamespacedName{Name: vg2SCName}, &storagev1.StorageClass{})
			}, 5*timeout, interval).WithContext(ctx).Should(Satisfy(k8serrors.IsNotFound))

			By("Verifying vg2 LVMVolumeGroup is deleted")
			Eventually(func(ctx SpecContext) error {
				return crClient.Get(ctx, types.NamespacedName{Name: "vg2", Namespace: installNamespace}, &v1alpha1.LVMVolumeGroup{})
			}, 5*timeout, interval).WithContext(ctx).Should(Satisfy(k8serrors.IsNotFound))

			By("Verifying vg2 is removed from LVMCluster status")
			Eventually(func(ctx SpecContext) bool {
				currentCluster := &v1alpha1.LVMCluster{}
				if err := crClient.Get(ctx, client.ObjectKeyFromObject(cluster), currentCluster); err != nil {
					return false
				}
				for _, dcs := range currentCluster.Status.DeviceClassStatuses {
					if dcs.Name == "vg2" {
						return false
					}
				}
				return true
			}, 5*timeout, interval).WithContext(ctx).Should(BeTrue())

			By("Verifying the cluster is still Ready with vg1")
			validateLVMCluster(ctx, cluster)
		})

		It("should also clean up VolumeSnapshotClass when removing a device class with thin pool", func(ctx SpecContext) {
			cluster = createClusterWithTwoDeviceClasses(ctx, cluster, true)

			vg2VSCName := resource.GetVolumeSnapshotClassName("vg2")

			By("Verifying vg2 VolumeSnapshotClass exists")
			Eventually(func(ctx SpecContext) error {
				err := crClient.Get(ctx, types.NamespacedName{Name: vg2VSCName}, &snapapi.VolumeSnapshotClass{})
				if meta.IsNoMatchError(err) {
					Skip("VolumeSnapshotClasses are not supported in this cluster")
				}
				return err
			}, timeout, interval).WithContext(ctx).Should(Succeed())

			By("Removing the non-default device class (vg2)")
			Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			cluster.Spec.Storage.DeviceClasses = filterDeviceClasses(cluster.Spec.Storage.DeviceClasses, "vg2")
			Expect(crClient.Update(ctx, cluster)).To(Succeed())

			By("Verifying vg2 StorageClass is deleted")
			Eventually(func(ctx SpecContext) error {
				return crClient.Get(ctx, types.NamespacedName{Name: resource.GetStorageClassName("vg2")}, &storagev1.StorageClass{})
			}, 5*timeout, interval).WithContext(ctx).Should(Satisfy(k8serrors.IsNotFound))

			By("Verifying vg2 VolumeSnapshotClass is deleted")
			Eventually(func(ctx SpecContext) bool {
				err := crClient.Get(ctx, types.NamespacedName{Name: vg2VSCName}, &snapapi.VolumeSnapshotClass{})
				return k8serrors.IsNotFound(err) || meta.IsNoMatchError(err)
			}, 5*timeout, interval).WithContext(ctx).Should(BeTrue())

			By("Verifying the cluster is still Ready")
			validateLVMCluster(ctx, cluster)
		})
	})

	// Rejected updates don't change server state, so all webhook tests share one cluster.
	Describe("Webhook Validation", Serial, Ordered, func() {
		var cluster *v1alpha1.LVMCluster

		BeforeAll(func(ctx SpecContext) {
			infraStatus, err := clusterstatus.GetClusterInfraStatus(ctx, config)
			Expect(err).NotTo(HaveOccurred(), "failed to get cluster infrastructure status")

			if infraStatus.ControlPlaneTopology == configv1.ExternalTopologyMode {
				Skip("Device class removal tests are not supported on HyperShift clusters")
			}
			if infraStatus.InfrastructureTopology != configv1.SingleReplicaTopologyMode {
				Skip("Device class removal tests run only on single-node (SNO) clusters")
			}

			waitForExistingClusterDeletion(ctx)
			cluster = GetDefaultTestLVMClusterTemplate()
			cluster = createClusterWithTwoDeviceClasses(ctx, cluster, false)
		})

		AfterAll(func(ctx SpecContext) {
			if cluster == nil {
				return
			}
			if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
				skipSuiteCleanup.Store(true)
			}
			DeleteResource(ctx, cluster)
			validateCSINodeInfo(ctx, cluster, false)
		})

		It("should reject removal of the default device class", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx SpecContext) {
				g.Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
				cluster.Spec.Storage.DeviceClasses = filterDeviceClasses(cluster.Spec.Storage.DeviceClasses, "vg1")
				err := crClient.Update(ctx, cluster)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("cannot delete default device class"))
			}, timeout, interval).WithContext(ctx).Should(Succeed())
		})

		It("should reject removal of all device classes", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx SpecContext) {
				g.Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
				cluster.Spec.Storage.DeviceClasses = []v1alpha1.DeviceClass{}
				err := crClient.Update(ctx, cluster)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("at least one device class must remain"))
			}, timeout, interval).WithContext(ctx).Should(Succeed())
		})
	})
}

// createClusterWithTwoDeviceClasses discovers available devices, deletes the initial cluster,
// and recreates it with vg1 (default, thick) and vg2 (non-default). If vg2WithThinPool is true,
// vg2 gets a ThinPoolConfig.
func createClusterWithTwoDeviceClasses(ctx SpecContext, cluster *v1alpha1.LVMCluster, vg2WithThinPool bool) *v1alpha1.LVMCluster {
	GinkgoHelper()

	By("Creating cluster without device selector to discover available devices")
	CreateResource(ctx, cluster)
	VerifyLVMSSetup(ctx, cluster)

	devices := getDiscoveredDevices(ctx, cluster)
	Expect(len(devices)).To(BeNumerically(">=", 2), "at least 2 devices are required for device class removal test")
	GinkgoLogr.Info("Discovered devices", "devices", devices)

	smallerDevice, largerDevice := classifyDevicesBySize(devices)

	By("Deleting cluster to recreate with two device classes")
	DeleteResource(ctx, cluster)
	validateCSINodeInfo(ctx, cluster, false)
	waitForExistingClusterDeletion(ctx)

	By("Recreating cluster with default (vg1) and non-default (vg2) device classes")
	cluster = GetDefaultTestLVMClusterTemplate()
	cluster.Spec.Storage.DeviceClasses[0].ThinPoolConfig = nil
	cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = &v1alpha1.DeviceSelector{
		Paths: []v1alpha1.DevicePath{v1alpha1.DevicePath(largerDevice)},
	}

	vg2 := v1alpha1.DeviceClass{
		Name:    "vg2",
		Default: false,
		DeviceSelector: &v1alpha1.DeviceSelector{
			Paths: []v1alpha1.DevicePath{v1alpha1.DevicePath(smallerDevice)},
		},
	}
	if vg2WithThinPool {
		vg2.ThinPoolConfig = &v1alpha1.ThinPoolConfig{
			Name:               "tp2",
			SizePercent:        90,
			OverprovisionRatio: 5,
		}
	}
	cluster.Spec.Storage.DeviceClasses = append(cluster.Spec.Storage.DeviceClasses, vg2)

	CreateResource(ctx, cluster)
	VerifyLVMSSetup(ctx, cluster)
	validateLVMVolumeGroupByName(ctx, "vg2")
	validateStorageClassByName(ctx, resource.GetStorageClassName("vg2"))

	return cluster
}

func filterDeviceClasses(classes []v1alpha1.DeviceClass, excludeName string) []v1alpha1.DeviceClass {
	var result []v1alpha1.DeviceClass
	for _, dc := range classes {
		if dc.Name != excludeName {
			result = append(result, dc)
		}
	}
	return result
}

func validateLVMVolumeGroupByName(ctx context.Context, name string) bool {
	GinkgoHelper()
	By("validating the LVMVolumeGroup " + name)
	return Eventually(func(ctx context.Context) error {
		return crClient.Get(ctx, types.NamespacedName{Name: name, Namespace: installNamespace}, &v1alpha1.LVMVolumeGroup{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

func validateStorageClassByName(ctx context.Context, name string) bool {
	GinkgoHelper()
	By("validating the StorageClass " + name)
	return Eventually(func(ctx context.Context) error {
		return crClient.Get(ctx, types.NamespacedName{Name: name}, &storagev1.StorageClass{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}
