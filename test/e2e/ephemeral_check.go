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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	//go:embed testdata/ephemeral_tests/pod-ephemeral-volume-device.yaml
	podEphemeralBlockYAMLTemplate string

	//go:embed testdata/ephemeral_tests/pod-ephemeral-volume-mount.yaml
	podEphemeralFSYAMLTemplate string

	//go:embed testdata/ephemeral_tests/ephemeral-volume-snapshot.yaml
	ephemeralVolumeSnapshotYAMLTemplate string

	//go:embed testdata/ephemeral_tests/ephemeral-clone.yaml
	ephemeralPvcCloneYAMLTemplate string

	//go:embed testdata/ephemeral_tests/ephemeral-snapshot-restore.yaml
	ephemeralPvcSnapshotRestoreYAMLTemplate string

	//go:embed testdata/ephemeral_tests/pod-volume-mount-template.yaml
	podFSYAMLTemplate string

	//go:embed testdata/ephemeral_tests/pod-volume-device-template.yaml
	podBlockYAMLTemplate string
)

func ephemeralTest() {
	Describe("Ephemeral Volume Tests", func() {
		var (
			pvc             = &k8sv1.PersistentVolumeClaim{}
			ephemeralPod    *k8sv1.Pod
			snapshot        *snapapi.VolumeSnapshot
			clonePvc        *k8sv1.PersistentVolumeClaim
			clonePod        *k8sv1.Pod
			restorePvc      *k8sv1.PersistentVolumeClaim
			restorePod      *k8sv1.Pod
			err             error
			skipSnapshotOps = false
		)

		Context("Create ephemeral pod and volume", func() {
			Context("Tests ephemeral volume operations for VolumeMode=Filesystem", Ordered, func() {
				It("Creation of ephemeral Pod binding a PVC from LVMS", func(ctx SpecContext) {
					By("Creating an ephemeral pod")
					podVolumeMountYaml := fmt.Sprintf(podEphemeralFSYAMLTemplate, "ephemeral-filepod", testNamespace, storageClassName)
					ephemeralPod, err = getPod(podVolumeMountYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, ephemeralPod)).To(Succeed())

					By("PVC should be bound")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-filepod-generic-ephemeral-volume", Namespace: testNamespace}, pvc); err != nil {
							return err
						}
						if pvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", pvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())

					By("Pod should be running")
					Eventually(func(ctx context.Context) bool {
						err = crClient.Get(ctx, types.NamespacedName{Name: ephemeralPod.Name, Namespace: testNamespace}, ephemeralPod)
						return err == nil && ephemeralPod.Status.Phase == k8sv1.PodRunning
					}, timeout, interval).WithContext(ctx).Should(BeTrue())
				})

				It("Testing Snapshot Operations", func(ctx SpecContext) {
					By("Creating a Snapshot of the pvc")
					snapshotYaml := fmt.Sprintf(ephemeralVolumeSnapshotYAMLTemplate, "ephemeralfilepvc-snapshot", testNamespace, snapshotClass, "ephemeral-filepod-generic-ephemeral-volume")
					snapshot, err = getVolumeSnapshot(snapshotYaml)
					Expect(err).To(BeNil())
					err = crClient.Create(ctx, snapshot)
					if discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
						skipSnapshotOps = true
						Skip("Skipping Testing of Snapshot Operations due to lack of volume snapshot support")
					}
					Expect(err).To(BeNil())

					By("Verifying that the Snapshot is ready")
					Eventually(func() bool {
						err := crClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
						return err == nil && snapshot.Status != nil && *snapshot.Status.ReadyToUse
					}, timeout, interval).Should(BeTrue())

					By("Creating a clone of the pvc")
					pvcCloneYaml := fmt.Sprintf(ephemeralPvcCloneYAMLTemplate, "ephemeralfilepvc-clone", testNamespace, "Filesystem", storageClassName, "ephemeral-filepod-generic-ephemeral-volume")
					clonePvc, err = getPVC(pvcCloneYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, clonePvc)).To(Succeed())

					By("Creating a pod consuming the clone of the pvc")
					podVolumeMountYaml := fmt.Sprintf(podFSYAMLTemplate, "clone-ephemeralfilepod", testNamespace, "ephemeralfilepvc-clone")
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

					By("Restore Snapshot for pvc")
					pvcRestoreYaml := fmt.Sprintf(ephemeralPvcSnapshotRestoreYAMLTemplate, "ephemeralfilepvc-restore", testNamespace, "Filesystem", storageClassName, "ephemeralfilepvc-snapshot")
					restorePvc, err = getPVC(pvcRestoreYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, restorePvc)).To(Succeed())

					By("Creating a pod consuming the restored snapshot data")
					podVolumeMountYaml = fmt.Sprintf(podFSYAMLTemplate, "restore-ephemeralfilepod", testNamespace, "ephemeralfilepvc-restore")
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

				It("Cleaning up ephemeral volume operations for VolumeMode=Filesystem", func(ctx SpecContext) {
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

					By("Deleting Pod")
					Expect(crClient.Delete(ctx, ephemeralPod)).To(Succeed())

					By("Confirming that ephemeral volume is automatically deleted")
					Eventually(func(ctx context.Context) bool {
						err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-filepod-generic-ephemeral-volume", Namespace: testNamespace}, pvc)
						return err != nil && k8serrors.IsNotFound(err)
					}, timeout, interval).WithContext(ctx).Should(BeTrue())
				})
			})

			Context("Tests PVC operations for VolumeMode=Block", Ordered, func() {
				It("Creation of ephemeral Pod binding a PVC from LVMS", func(ctx SpecContext) {
					By("Creating an ephemeral pod")
					podVolumeBlockYaml := fmt.Sprintf(podEphemeralBlockYAMLTemplate, "ephemeral-blockpod", testNamespace, storageClassName)
					ephemeralPod, err = getPod(podVolumeBlockYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, ephemeralPod)).To(Succeed())

					By("PVC should be bound")
					Eventually(func(ctx context.Context) error {
						if err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-blockpod-generic-ephemeral-volume", Namespace: testNamespace}, pvc); err != nil {
							return err
						}
						if pvc.Status.Phase != k8sv1.ClaimBound {
							return fmt.Errorf("pvc is not bound yet: %s", pvc.Status.Phase)
						}
						return nil
					}, timeout, interval).WithContext(ctx).Should(Succeed())

					By("Pod should be running")
					Eventually(func(ctx context.Context) bool {
						err = crClient.Get(ctx, types.NamespacedName{Name: ephemeralPod.Name, Namespace: testNamespace}, ephemeralPod)
						return err == nil && ephemeralPod.Status.Phase == k8sv1.PodRunning
					}, timeout, interval).WithContext(ctx).Should(BeTrue())
				})

				It("Testing Snapshot Operations", func(ctx SpecContext) {
					By("Creating a Snapshot of the pvc")
					snapshotYaml := fmt.Sprintf(ephemeralVolumeSnapshotYAMLTemplate, "ephemeralblockpvc-snapshot", testNamespace, snapshotClass, "ephemeral-blockpod-generic-ephemeral-volume")
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
						return err == nil && snapshot.Status != nil && *snapshot.Status.ReadyToUse
					}, timeout, interval).WithContext(ctx).Should(BeTrue())

					By("Creating a clone of the pvc")
					pvcCloneYaml := fmt.Sprintf(ephemeralPvcCloneYAMLTemplate, "ephemeralblockpvc-clone", testNamespace, "Block", storageClassName, "ephemeral-blockpod-generic-ephemeral-volume")
					clonePvc, err = getPVC(pvcCloneYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, clonePvc)).To(BeNil())

					By("Creating a pod consuming the clone of the pvc")
					podVolumeBlockYaml := fmt.Sprintf(podBlockYAMLTemplate, "clone-ephemeralblockpod", testNamespace, "ephemeralblockpvc-clone")
					clonePod, err = getPod(podVolumeBlockYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, clonePod)).To(BeNil())

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

					By("Restore Snapshot for pvc")
					pvcRestoreYaml := fmt.Sprintf(ephemeralPvcSnapshotRestoreYAMLTemplate, "ephemeralblockpvc-restore", testNamespace, "Block", storageClassName, "ephemeralblockpvc-snapshot")
					restorePvc, err = getPVC(pvcRestoreYaml)
					Expect(err).To(BeNil())
					Expect(crClient.Create(ctx, restorePvc)).To(Succeed())

					By("Creating a pod consuming the restored snapshot data")
					podVolumeBlockYaml = fmt.Sprintf(podBlockYAMLTemplate, "restore-ephemeralblockpod", testNamespace, "ephemeralblockpvc-restore")
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

				It("Cleaning up ephemeral volume operations for VolumeMode=Filesystem", func(ctx SpecContext) {
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

					By("Deleting Pod")
					Expect(crClient.Delete(ctx, ephemeralPod)).To(Succeed())

					By("Confirming that ephemeral volume is automatically deleted")
					Eventually(func(ctx context.Context) bool {
						err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-blockpod-generic-ephemeral-volume", Namespace: testNamespace}, pvc)
						return err != nil && k8serrors.IsNotFound(err)
					}, timeout, interval).WithContext(ctx).Should(BeTrue())
				})
			})

		})
	})
}
