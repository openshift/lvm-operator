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
	_ "embed"
	"errors"
	"fmt"
	"k8s.io/client-go/discovery"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed testdata/pvc_tests/pvc-template.yaml
var pvcYAMLTemplate string

//go:embed testdata/pvc_tests/pod-volume-mount-template.yaml
var podVolumeFSYAMLTemplate string

//go:embed testdata/pvc_tests/pod-volume-device-template.yaml
var podVolumeBlockYAMLTemplate string

//go:embed testdata/pvc_tests/volume-snapshot-template.yaml
var volumeSnapshotYAMLTemplate string

//go:embed testdata/pvc_tests/pvc-clone-template.yaml
var pvcCloneYAMLTemplate string

//go:embed testdata/pvc_tests/pvc-snapshot-restore-template.yaml
var pvcSnapshotRestoreYAMLTemplate string

func pvcTest() {

	Describe("PVC Tests", func() {
		var pvc *k8sv1.PersistentVolumeClaim
		var pod *k8sv1.Pod
		var snapshot *snapapi.VolumeSnapshot
		var clonePvc *k8sv1.PersistentVolumeClaim
		var clonePod *k8sv1.Pod
		var restorePvc *k8sv1.PersistentVolumeClaim
		var restorePod *k8sv1.Pod
		var err error

		var skipSnapshotOps bool

		Context("create pvc, pod, snapshots, clones", Ordered, func() {
			Context("Tests PVC operations for VolumeMode=Filesystem", Ordered, func() {
				It("Creation of Pod binding a PVC from LVMS", func(ctx SpecContext) {
					By("Creating a pvc")
					filePvcYaml := fmt.Sprintf(pvcYAMLTemplate, "lvmfilepvc", testNamespace, "Filesystem", storageClassName)
					pvc, err = getPVC(filePvcYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, pvc)).To(Succeed())

					By("Creating a pod")
					podVolumeMountYaml := fmt.Sprintf(podVolumeFSYAMLTemplate, "lvmfilepod", testNamespace, "lvmfilepvc")
					pod, err = getPod(podVolumeMountYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, pod)).To(Succeed())

					By("Verifying that the PVC(file system) is bound and the Pod is running")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, pvc); err != nil {
							return err
						}
						if pvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", pvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())

					By("Pod should be running")
					Eventually(func(ctx context.Context) bool {
						err = crClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
						return err == nil && pod.Status.Phase == k8sv1.PodRunning
					}, timeout, interval).WithContext(ctx).Should(BeTrue())
				})

				It("Testing Snapshot Operations", func(ctx SpecContext) {
					By("Creating a Snapshot of the file-pvc")
					snapshotYaml := fmt.Sprintf(volumeSnapshotYAMLTemplate, "lvmfilepvc-snapshot", testNamespace, snapshotClass, "lvmfilepvc")
					snapshot, err = getVolumeSnapshot(snapshotYaml)
					Expect(err).To(BeNil())
					err = crClient.Create(ctx, snapshot)
					if discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
						skipSnapshotOps = true
						Skip("Skipping Testing of Snapshot Operations due to lack of volume snapshot support")
					}
					Expect(err).To(BeNil())

					By("Verifying that the Snapshot is ready")
					Eventually(func(ctx context.Context) bool {
						err := crClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
						if err == nil && snapshot.Status != nil && snapshot.Status.ReadyToUse != nil {
							return *snapshot.Status.ReadyToUse
						}
						return false
					}, timeout, interval).WithContext(ctx).Should(BeTrue())

					By("Creating a clone of the filesystem pvc")
					pvcCloneYaml := fmt.Sprintf(pvcCloneYAMLTemplate, "lvmfilepvc-clone", testNamespace, "Filesystem", storageClassName, "lvmfilepvc")
					clonePvc, err = getPVC(pvcCloneYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, clonePvc)).To(Succeed())

					By("Creating a pod consuming the clone")
					podVolumeMountYaml := fmt.Sprintf(podVolumeFSYAMLTemplate, "clone-lvmfilepod", testNamespace, "lvmfilepvc-clone")
					clonePod, err = getPod(podVolumeMountYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, clonePod)).To(Succeed())

					By("Having a bound claim in the pvc")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, client.ObjectKeyFromObject(clonePvc), clonePvc); err != nil {
							return err
						}
						if clonePvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", clonePvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())

					By("Restore Snapshot for file-pvc")
					pvcRestoreYaml := fmt.Sprintf(pvcSnapshotRestoreYAMLTemplate, "lvmfilepvc-restore", testNamespace, "Filesystem", storageClassName, "lvmfilepvc-snapshot")
					restorePvc, err = getPVC(pvcRestoreYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, restorePvc)).To(Succeed())

					By("Creating a pod consuming the restored snapshot of the pvc")
					podVolumeMountYaml = fmt.Sprintf(podVolumeFSYAMLTemplate, "restore-lvmfilepod", testNamespace, "lvmfilepvc-restore")
					restorePod, err = getPod(podVolumeMountYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, restorePod)).To(Succeed())

					By("Having the restored data pvc be bound")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, client.ObjectKeyFromObject(restorePvc), restorePvc); err != nil {
							return err
						}
						if restorePvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", restorePvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())
				})

				It("Cleaning up for VolumeMode=Filesystem", func(ctx SpecContext) {
					if !skipSnapshotOps {
						By(fmt.Sprintf("Deleting %s", clonePod.Name))
						Expect(crClient.Delete(ctx, clonePod)).To(Succeed())

						By(fmt.Sprintf("Deleting Clone PVC %s", clonePvc.Name))
						Expect(crClient.Delete(ctx, clonePvc)).To(Succeed())

						By(fmt.Sprintf("Deleting Pod %s", restorePod.Name))
						Expect(crClient.Delete(ctx, restorePod)).To(Succeed())

						By(fmt.Sprintf("Deleting Snapshot PVC %s", restorePvc.Name))
						Expect(crClient.Delete(ctx, restorePvc)).To(Succeed())

						By(fmt.Sprintf("Deleting VolumeSnapshot %s", snapshot.Name))
						Expect(crClient.Delete(ctx, snapshot)).To(Succeed())
					}

					By(fmt.Sprintf("Deleting Pod %s", pod.Name))
					Expect(crClient.Delete(ctx, pod)).To(Succeed())

					By(fmt.Sprintf("Deleting PVC %s", pvc.Name))
					Expect(crClient.Delete(ctx, pvc)).To(Succeed())
				})
			})

			Context("Tests PVC operations for VolumeMode=Block", Ordered, func() {
				It("Creation of Pod binding a PVC from LVMS", func(ctx SpecContext) {
					By("Creating a pvc")
					blockPvcYaml := fmt.Sprintf(pvcYAMLTemplate, "lvmblockpvc", testNamespace, "Block", storageClassName)
					pvc, err = getPVC(blockPvcYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, pvc)).To(Succeed())

					By("Creating a pod")
					podVolumeBlockYaml := fmt.Sprintf(podVolumeBlockYAMLTemplate, "lvmblockpod", testNamespace, "lvmblockpvc")
					pod, err = getPod(podVolumeBlockYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, pod)).To(Succeed())

					By("Verifying that the PVC(block) is bound and the Pod is running")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil {
							return err
						}
						if pvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", pvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())

					By("Pod should be running")
					Eventually(func(ctx context.Context) bool {
						err = crClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
						return err == nil && pod.Status.Phase == k8sv1.PodRunning
					}, timeout, interval).WithContext(ctx).Should(BeTrue())
				})

				It("Testing Snapshot Operations", func(ctx SpecContext) {
					By("Creating a Snapshot of the block-pvc")
					snapshotYaml := fmt.Sprintf(volumeSnapshotYAMLTemplate, "lvmblockpvc-snapshot", testNamespace, snapshotClass, "lvmblockpvc")
					snapshot, err = getVolumeSnapshot(snapshotYaml)
					Expect(err).To(BeNil())
					err = crClient.Create(ctx, snapshot)
					if discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
						skipSnapshotOps = true
						Skip("Skipping Testing of Snapshot Operations due to lack of volume snapshot support")
					}
					Expect(err).To(BeNil())

					By("Verifying that the Snapshot is ready")
					Eventually(func(ctx context.Context) bool {
						err := crClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
						if err == nil && snapshot.Status != nil && snapshot.Status.ReadyToUse != nil {
							return *snapshot.Status.ReadyToUse
						}
						return false
					}, timeout, interval).WithContext(ctx).Should(BeTrue())

					By("Creating a clone of the block-pvc")
					pvcCloneYaml := fmt.Sprintf(pvcCloneYAMLTemplate, "lvmblockpvc-clone", testNamespace, "Block", storageClassName, "lvmblockpvc")
					clonePvc, err = getPVC(pvcCloneYaml)
					Expect(err).To(BeNil())
					err = crClient.Create(ctx, clonePvc)
					Expect(err).To(BeNil())

					By("Creating a pod consuming the clone of the pvc")
					podVolumeBlockYaml := fmt.Sprintf(podVolumeBlockYAMLTemplate, "clone-lvmblockpod", testNamespace, "lvmblockpvc-clone")
					clonePod, err = getPod(podVolumeBlockYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, clonePod)).To(Succeed())

					By("Having a bound claim in the pvc")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, client.ObjectKeyFromObject(clonePvc), clonePvc); err != nil {
							return err
						}
						if clonePvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", clonePvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())

					By("Restore Snapshot for block-pvc")
					pvcRestoreYaml := fmt.Sprintf(pvcSnapshotRestoreYAMLTemplate, "lvmblockpvc-restore", testNamespace, "Block", storageClassName, "lvmblockpvc-snapshot")
					restorePvc, err = getPVC(pvcRestoreYaml)
					Expect(err).To(BeNil())
					err = crClient.Create(ctx, restorePvc)
					Expect(err).To(BeNil())

					By("Creating a pod consuming the restored snapshot data")
					podVolumeBlockYaml = fmt.Sprintf(podVolumeBlockYAMLTemplate, "restore-lvmblockpod", testNamespace, "lvmblockpvc-restore")
					restorePod, err = getPod(podVolumeBlockYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, restorePod)).To(Succeed())

					By("Having the restored data pvc be bound")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, client.ObjectKeyFromObject(restorePvc), restorePvc); err != nil {
							return err
						}
						if restorePvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", restorePvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())
				})

				It("Cleaning up for VolumeMode=Block", func(ctx SpecContext) {
					if !skipSnapshotOps {
						By(fmt.Sprintf("Deleting %s", clonePod.Name))
						Expect(crClient.Delete(ctx, clonePod)).To(Succeed())

						By(fmt.Sprintf("Deleting Clone PVC %s", clonePvc.Name))
						Expect(crClient.Delete(ctx, clonePvc)).To(Succeed())

						By(fmt.Sprintf("Deleting Pod %s", restorePod.Name))
						Expect(crClient.Delete(ctx, restorePod)).To(Succeed())

						By(fmt.Sprintf("Deleting Snapshot PVC %s", restorePvc.Name))
						Expect(crClient.Delete(ctx, restorePvc)).To(Succeed())

						By(fmt.Sprintf("Deleting VolumeSnapshot %s", snapshot.Name))
						Expect(crClient.Delete(ctx, snapshot)).To(Succeed())
					}

					By(fmt.Sprintf("Deleting Pod %s", pod.Name))
					Expect(crClient.Delete(ctx, pod)).To(Succeed())

					By(fmt.Sprintf("Deleting PVC %s", pvc.Name))
					Expect(crClient.Delete(ctx, pvc)).To(Succeed())
				})
			})

		})
	})
}
