package e2e

import (
	"context"
	_ "embed"
	"fmt"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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
			pvc          *k8sv1.PersistentVolumeClaim = &k8sv1.PersistentVolumeClaim{}
			ephemeralPod *k8sv1.Pod
			snapshot     *snapapi.VolumeSnapshot
			clonePvc     *k8sv1.PersistentVolumeClaim
			clonePod     *k8sv1.Pod
			restorePvc   *k8sv1.PersistentVolumeClaim
			restorePod   *k8sv1.Pod
			err          error
			ctx          = context.Background()
		)

		Context("Create ephemeral pod and volume", func() {
			It("Tests ephemeral volume operations for VolumeMode=Filesystem", func() {

				By("Creating a pod with generic ephemeral volume")
				podVolumeMountYaml := fmt.Sprintf(podEphemeralFSYAMLTemplate, "ephemeral-filepod", testNamespace, storageClass)
				ephemeralPod, err = getPod(podVolumeMountYaml)
				err = crClient.Create(ctx, ephemeralPod)
				Expect(err).To(BeNil())

				By("PVC should be bound")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-filepod-generic-ephemeral-volume", Namespace: testNamespace}, pvc)
					return err == nil && pvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", pvc.Name)

				By("Pod should be running")
				Eventually(func() bool {
					err = crClient.Get(ctx, types.NamespacedName{Name: ephemeralPod.Name, Namespace: testNamespace}, ephemeralPod)
					return err == nil && ephemeralPod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", ephemeralPod.Name)

				By("Creating a Snapshot of the pvc")
				snapshotYaml := fmt.Sprintf(ephemeralVolumeSnapshotYAMLTemplate, "ephemeralfilepvc-snapshot", testNamespace, snapshotClass, "ephemeral-filepod-generic-ephemeral-volume")
				snapshot, err = getVolumeSnapshot(snapshotYaml)
				err = crClient.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Verifying that the Snapshot is ready")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
					return err == nil && snapshot.Status != nil && *snapshot.Status.ReadyToUse
				}, timeout, interval).Should(BeTrue())

				By("Creating a clone of the pvc")
				pvcCloneYaml := fmt.Sprintf(ephemeralPvcCloneYAMLTemplate, "ephemeralfilepvc-clone", testNamespace, "Filesystem", storageClass, "ephemeral-filepod-generic-ephemeral-volume")
				clonePvc, err = getPVC(pvcCloneYaml)
				err = crClient.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				podVolumeMountYaml = fmt.Sprintf(podFSYAMLTemplate, "clone-ephemeralfilepod", testNamespace, "ephemeralfilepvc-clone")
				clonePod, err = getPod(podVolumeMountYaml)
				err = crClient.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for pvc")
				pvcRestoreYaml := fmt.Sprintf(ephemeralPvcSnapshotRestoreYAMLTemplate, "ephemeralfilepvc-restore", testNamespace, "Filesystem", storageClass, "ephemeralfilepvc-snapshot")
				restorePvc, err = getPVC(pvcRestoreYaml)
				err = crClient.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				podVolumeMountYaml = fmt.Sprintf(podFSYAMLTemplate, "restore-ephemeralfilepod", testNamespace, "ephemeralfilepvc-restore")
				restorePod, err = getPod(podVolumeMountYaml)
				err = crClient.Create(ctx, restorePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: restorePvc.Name, Namespace: restorePvc.Namespace}, restorePvc)
					return err == nil && restorePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Restored PVC %s is bound\n", restorePvc.Name)

				err = crClient.Delete(ctx, clonePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", clonePod.Name)

				err = crClient.Delete(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Clone PVC %s is deleted\n", clonePvc.Name)

				err = crClient.Delete(ctx, restorePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", restorePod.Name)

				err = crClient.Delete(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Restored Snapshot %s is deleted\n", restorePvc.Name)

				err = crClient.Delete(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is deleted\n", snapshot.Name)

				By("Deleting the pod")
				err = crClient.Delete(ctx, ephemeralPod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", ephemeralPod.Name)

				By("Confirming that ephemeral volume is automatically deleted")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-filepod-generic-ephemeral-volume", Namespace: testNamespace}, pvc)
					return err != nil && errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Deleting the pod, deleted the ephemeral volume %s\n", pvc.Name)

			})

			It("Tests PVC operations for VolumeMode=Block", func() {
				By("Creating a pod with generic ephemeral volume")
				podVolumeBlockYaml := fmt.Sprintf(podEphemeralBlockYAMLTemplate, "ephemeral-blockpod", testNamespace, storageClass)
				ephemeralPod, err = getPod(podVolumeBlockYaml)
				err = crClient.Create(ctx, ephemeralPod)
				Expect(err).To(BeNil())

				By("PVC should be bound")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-blockpod-generic-ephemeral-volume", Namespace: testNamespace}, pvc)
					return err == nil && pvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", pvc.Name)

				By("Pod should be running")
				Eventually(func() bool {
					err = crClient.Get(ctx, types.NamespacedName{Name: ephemeralPod.Name, Namespace: testNamespace}, ephemeralPod)
					return err == nil && ephemeralPod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", ephemeralPod.Name)

				By("Creating a Snapshot of the pvc")
				snapshotYaml := fmt.Sprintf(ephemeralVolumeSnapshotYAMLTemplate, "ephemeralblockpvc-snapshot", testNamespace, snapshotClass, "ephemeral-blockpod-generic-ephemeral-volume")
				snapshot, err = getVolumeSnapshot(snapshotYaml)
				err = crClient.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Verifying that the Snapshot is ready")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
					return err == nil && snapshot.Status != nil && *snapshot.Status.ReadyToUse
				}, timeout, interval).Should(BeTrue())

				By("Creating a clone of the pvc")
				pvcCloneYaml := fmt.Sprintf(ephemeralPvcCloneYAMLTemplate, "ephemeralblockpvc-clone", testNamespace, "Block", storageClass, "ephemeral-blockpod-generic-ephemeral-volume")
				clonePvc, err = getPVC(pvcCloneYaml)
				err = crClient.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				podVolumeBlockYaml = fmt.Sprintf(podBlockYAMLTemplate, "clone-ephemeralblockpod", testNamespace, "ephemeralblockpvc-clone")
				clonePod, err = getPod(podVolumeBlockYaml)
				err = crClient.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for pvc")
				pvcRestoreYaml := fmt.Sprintf(ephemeralPvcSnapshotRestoreYAMLTemplate, "ephemeralblockpvc-restore", testNamespace, "Block", storageClass, "ephemeralblockpvc-snapshot")
				restorePvc, err = getPVC(pvcRestoreYaml)
				err = crClient.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				podVolumeBlockYaml = fmt.Sprintf(podBlockYAMLTemplate, "restore-ephemeralblockpod", testNamespace, "ephemeralblockpvc-restore")
				restorePod, err = getPod(podVolumeBlockYaml)
				err = crClient.Create(ctx, restorePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: restorePvc.Name, Namespace: restorePvc.Namespace}, restorePvc)
					return err == nil && restorePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Restored PVC %s is bound\n", restorePvc.Name)

				err = crClient.Delete(ctx, clonePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", clonePod.Name)

				err = crClient.Delete(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Clone PVC %s is deleted\n", clonePvc.Name)

				err = crClient.Delete(ctx, restorePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", restorePod.Name)

				err = crClient.Delete(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Restored Snapshot %s is deleted\n", restorePvc.Name)

				err = crClient.Delete(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is deleted\n", snapshot.Name)

				By("Deleting the pod")
				err = crClient.Delete(ctx, ephemeralPod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", ephemeralPod.Name)

				By("Confirming that ephemeral volume is automatically deleted")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: "ephemeral-blockpod-generic-ephemeral-volume", Namespace: testNamespace}, pvc)
					return err != nil && errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Deleting the pod, deleted the ephemeral volume\n")
			})

		})
	})
}
