package e2e

import (
	"context"
	"fmt"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	size = "5Gi"
)

func pvcTest() {

	Describe("PVC Tests", func() {
		var pvc *k8sv1.PersistentVolumeClaim
		var pod *k8sv1.Pod
		var snapshot *snapapi.VolumeSnapshot
		var clonePvc *k8sv1.PersistentVolumeClaim
		var clonePod *k8sv1.Pod
		var restorePvc *k8sv1.PersistentVolumeClaim
		var restorePod *k8sv1.Pod
		ctx := context.Background()

		Context("create pvc, pod, snapshots, clones", func() {
			It("Tests PVC operations for VolumeMode=file system", func() {
				By("Creating pvc and pod")
				pvc = getSamplePvc(size, "lvmfilepvc", k8sv1.PersistentVolumeFilesystem, storageClass, "", "")
				pod = getSamplePod("lvmfilepod", pvc.Name)
				err := crClient.Create(ctx, pvc)
				Expect(err).To(BeNil())
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
				snapshot = getSampleVolumeSnapshot(pvc.Name+"-snapshot", pvc.Name, storageClass)
				err = crClient.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Creating a clone of the file-pvc")
				clonePvc = getSamplePvc(size, pvc.Name+"-clone", k8sv1.PersistentVolumeFilesystem, storageClass, "PersistentVolumeClaim", pvc.Name)
				err = crClient.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				clonePod = getSamplePod("clone-lvmfilepod", clonePvc.Name)
				err = crClient.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for file-pvc")
				restorePvc = getSamplePvc(size, pvc.Name+"-restore", k8sv1.PersistentVolumeFilesystem, storageClass, "VolumeSnapshot", snapshot.Name)
				err = crClient.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				restorePod = getSamplePod("restore-lvmfilepod", restorePvc.Name)
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
				pvc = getSamplePvc(size, "lvmblockpvc", k8sv1.PersistentVolumeBlock, storageClass, "", "")
				pod = getSamplePod("lvmblockpod", pvc.Name)
				err := crClient.Create(ctx, pvc)
				Expect(err).To(BeNil())
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
				snapshot = getSampleVolumeSnapshot(pvc.Name+"-snapshot", pvc.Name, storageClass)
				err = crClient.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Creating a clone of the block-pvc")
				clonePvc = getSamplePvc(size, pvc.Name+"-clone", k8sv1.PersistentVolumeBlock, storageClass, "PersistentVolumeClaim", pvc.Name)
				err = crClient.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				clonePod = getSamplePod("clone-lvmblockpod", clonePvc.Name)
				err = crClient.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := crClient.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for block-pvc")
				restorePvc = getSamplePvc(size, pvc.Name+"-restore", k8sv1.PersistentVolumeBlock, storageClass, "VolumeSnapshot", snapshot.Name)
				err = crClient.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				restorePod = getSamplePod("restore-lvmblockpod", restorePvc.Name)
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
