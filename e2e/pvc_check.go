package e2e

import (
	"context"
	_ "embed"
	"fmt"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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
		ctx := context.Background()

		Context("create pvc, pod, snapshots, clones", func() {
			It("Tests PVC operations for VolumeMode=Filesystem", func() {
				By("Creating a pvc and pod")
				filePvcYaml := fmt.Sprintf(pvcYAMLTemplate, "lvmfilepvc", testNamespace, "Filesystem", storageClass)
				pvc, err = getPVC(filePvcYaml)
				err = crClient.Create(ctx, pvc)
				Expect(err).To(BeNil())

				podVolumeMountYaml := fmt.Sprintf(podVolumeFSYAMLTemplate, "lvmfilepod", testNamespace, "lvmfilepvc")
				pod, err = getPod(podVolumeMountYaml)
				err = crClient.Create(ctx, pod)
				Expect(err).To(BeNil())

				By("Verifying that the PVC(file system) is bound and the Pod is running")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, pvc)
					return err == nil && pvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", pvc.Name)

				Eventually(func() bool {
					err = crClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
					return err == nil && pod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", pod.Name)

				By("Creating a Snapshot of the file-pvc")
				snapshotYaml := fmt.Sprintf(volumeSnapshotYAMLTemplate, "lvmfilepvc-snapshot", testNamespace, snapshotClass, "lvmfilepvc")
				snapshot, err = getVolumeSnapshot(snapshotYaml)
				err = crClient.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Verifying that the Snapshot is ready")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
					return err == nil && *snapshot.Status.ReadyToUse
				}, timeout, interval).Should(BeTrue())

				By("Creating a clone of the filesystem pvc")
				pvcCloneYaml := fmt.Sprintf(pvcCloneYAMLTemplate, "lvmfilepvc-clone", testNamespace, "Filesystem", storageClass, "lvmfilepvc")
				clonePvc, err = getPVC(pvcCloneYaml)
				err = crClient.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				podVolumeMountYaml = fmt.Sprintf(podVolumeFSYAMLTemplate, "clone-lvmfilepod", testNamespace, "lvmfilepvc-clone")
				clonePod, err = getPod(podVolumeMountYaml)
				err = crClient.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for file-pvc")
				pvcRestoreYaml := fmt.Sprintf(pvcSnapshotRestoreYAMLTemplate, "lvmfilepvc-restore", testNamespace, "Filesystem", storageClass, "lvmfilepvc-snapshot")
				restorePvc, err = getPVC(pvcRestoreYaml)
				err = crClient.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				podVolumeMountYaml = fmt.Sprintf(podVolumeFSYAMLTemplate, "restore-lvmfilepod", testNamespace, "lvmfilepvc-restore")
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

				err = crClient.Delete(ctx, pod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", pod.Name)

				err = crClient.Delete(ctx, pvc)
				Expect(err).To(BeNil())
				fmt.Printf("PVC %s is deleted\n", pvc.Name)
			})

			It("Tests PVC operations for VolumeMode=Block", func() {
				By("Creating pvc and pod")
				blockPvcYaml := fmt.Sprintf(pvcYAMLTemplate, "lvmblockpvc", testNamespace, "Block", storageClass)
				pvc, err = getPVC(blockPvcYaml)
				err = crClient.Create(ctx, pvc)
				Expect(err).To(BeNil())

				podVolumeBlockYaml := fmt.Sprintf(podVolumeBlockYAMLTemplate, "lvmblockpod", testNamespace, "lvmblockpvc")
				pod, err = getPod(podVolumeBlockYaml)
				err = crClient.Create(ctx, pod)
				Expect(err).To(BeNil())

				By("Verifying that the PVC(block) is bound and the Pod is running")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, pvc)
					return err == nil && pvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", pvc.Name)

				Eventually(func() bool {
					err = crClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
					return err == nil && pod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", pod.Name)

				By("Creating a Snapshot of the block-pvc")
				snapshotYaml := fmt.Sprintf(volumeSnapshotYAMLTemplate, "lvmblockpvc-snapshot", testNamespace, snapshotClass, "lvmblockpvc")
				snapshot, err = getVolumeSnapshot(snapshotYaml)
				err = crClient.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Verifying that the Snapshot is ready")
				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
					return err == nil && *snapshot.Status.ReadyToUse
				}, timeout, interval).Should(BeTrue())

				By("Creating a clone of the block-pvc")
				pvcCloneYaml := fmt.Sprintf(pvcCloneYAMLTemplate, "lvmblockpvc-clone", testNamespace, "Block", storageClass, "lvmblockpvc")
				clonePvc, err = getPVC(pvcCloneYaml)
				err = crClient.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				podVolumeBlockYaml = fmt.Sprintf(podVolumeBlockYAMLTemplate, "clone-lvmblockpod", testNamespace, "lvmblockpvc-clone")
				clonePod, err = getPod(podVolumeBlockYaml)
				err = crClient.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for block-pvc")
				pvcRestoreYaml := fmt.Sprintf(pvcSnapshotRestoreYAMLTemplate, "lvmblockpvc-restore", testNamespace, "Block", storageClass, "lvmblockpvc-snapshot")
				restorePvc, err = getPVC(pvcRestoreYaml)
				err = crClient.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				podVolumeBlockYaml = fmt.Sprintf(podVolumeBlockYAMLTemplate, "restore-lvmblockpod", testNamespace, "lvmblockpvc-restore")
				restorePod, err = getPod(podVolumeBlockYaml)
				err = crClient.Create(ctx, restorePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: restorePvc.Name, Namespace: restorePvc.Namespace}, restorePvc)
					return err == nil && restorePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Restored PVC %s is bound\n", restorePod.Name)

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

				err = crClient.Delete(ctx, pod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", pod.Name)

				err = crClient.Delete(ctx, pvc)
				Expect(err).To(BeNil())
				fmt.Printf("PVC %s is deleted\n", pvc.Name)
			})
		})
	})
}
