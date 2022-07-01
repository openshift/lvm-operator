package lvm_test

import (
	"context"
	"fmt"
	"time"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tests "github.com/red-hat-storage/lvm-operator/e2e"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	timeout  = time.Minute * 5
	interval = time.Second * 30
	size     = "5Gi"
)

func PVCTest() {

	Describe("PVC Tests", func() {
		var pvc *k8sv1.PersistentVolumeClaim
		var pod *k8sv1.Pod
		var snapshot *snapapi.VolumeSnapshot
		var clonePvc *k8sv1.PersistentVolumeClaim
		var clonePod *k8sv1.Pod
		var restorePvc *k8sv1.PersistentVolumeClaim
		var restorePod *k8sv1.Pod
		client := tests.DeployManagerObj.GetCrClient()
		ctx := context.TODO()

		Context("create pvc, pod, snapshots, clones", func() {
			It("Tests PVC operations for VolumeMode=file system", func() {
				By("Creating pvc and pod")
				pvc = tests.GetSamplePvc(size, "lvmfilepvc", k8sv1.PersistentVolumeFilesystem, tests.StorageClass, "", "")
				pod = tests.GetSamplePod("lvmfilepod", pvc.Name)
				err := client.Create(ctx, pvc)
				Expect(err).To(BeNil())
				err = client.Create(ctx, pod)
				Expect(err).To(BeNil())

				By("Verifying that the PVC(file system) is bound and the Pod is running")
				Eventually(func() bool {
					err := client.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, pvc)
					return err == nil && pvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", pvc.Name)

				Eventually(func() bool {
					err = client.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
					return err == nil && pod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", pod.Name)

				By("Creating a Snapshot of the file-pvc")
				snapshot = tests.GetSampleVolumeSnapshot(pvc.Name, tests.StorageClass)
				err = client.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Creating a clone of the file-pvc")
				clonePvc = tests.GetSamplePvc(size, pvc.Name, k8sv1.PersistentVolumeFilesystem, tests.StorageClass, "PersistentVolumeClaim", pvc.Name)
				err = client.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				clonePod = tests.GetSamplePod("clone-lvmfilepod", clonePvc.Name)
				err = client.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := client.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for file-pvc")
				restorePvc = tests.GetSamplePvc(size, pvc.Name, k8sv1.PersistentVolumeFilesystem, tests.StorageClass, "VolumeSnapshot", pvc.Name)
				err = client.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				restorePod = tests.GetSamplePod("restore-lvmfilepod", restorePvc.Name)
				err = client.Create(ctx, restorePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := client.Get(ctx, types.NamespacedName{Name: restorePvc.Name, Namespace: restorePvc.Namespace}, restorePvc)
					return err == nil && restorePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Restored PVC %s is bound\n", restorePvc.Name)

				err = client.Delete(ctx, clonePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", clonePod.Name)

				err = client.Delete(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Clone PVC %s is deleted\n", clonePvc.Name)

				err = client.Delete(ctx, restorePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", restorePod.Name)

				err = client.Delete(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Restored Snapshot %s is deleted\n", restorePvc.Name)

				err = client.Delete(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is deleted\n", snapshot.Name)

				err = client.Delete(ctx, pod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", pod.Name)

				err = client.Delete(ctx, pvc)
				Expect(err).To(BeNil())
				fmt.Printf("PVC %s is deleted\n", pvc.Name)
			})

			It("Tests PVC operations for VolumeMode=Block", func() {
				By("Creating pvc and pod")
				pvc = tests.GetSamplePvc(size, "lvmblockpvc", k8sv1.PersistentVolumeBlock, tests.StorageClass, "", "")
				pod = tests.GetSamplePod("lvmblockpod", pvc.Name)
				err := client.Create(ctx, pvc)
				Expect(err).To(BeNil())
				err = client.Create(ctx, pod)
				Expect(err).To(BeNil())

				By("Verifying that the PVC(block) is bound and the Pod is running")
				Eventually(func() bool {
					err := client.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, pvc)
					return err == nil && pvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("PVC %s is bound\n", pvc.Name)

				Eventually(func() bool {
					err = client.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
					return err == nil && pod.Status.Phase == k8sv1.PodRunning
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Pod %s is running\n", pod.Name)

				By("Creating a Snapshot of the block-pvc")
				snapshot = tests.GetSampleVolumeSnapshot(pvc.Name, tests.StorageClass)
				err = client.Create(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is created\n", snapshot.Name)

				By("Creating a clone of the block-pvc")
				clonePvc = tests.GetSamplePvc(size, pvc.Name, k8sv1.PersistentVolumeBlock, tests.StorageClass, "PersistentVolumeClaim", pvc.Name)
				err = client.Create(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Cloned PVC %s is created\n", clonePvc.Name)

				clonePod = tests.GetSamplePod("clone-lvmblockpod", "lvmblockpvcclone")
				err = client.Create(ctx, clonePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := client.Get(ctx, types.NamespacedName{Name: clonePvc.Name, Namespace: clonePvc.Namespace}, clonePvc)
					return err == nil && clonePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Cloned PVC %s is bound\n", clonePvc.Name)

				By("Restore Snapshot for block-pvc")
				restorePvc = tests.GetSamplePvc(size, pvc.Name, k8sv1.PersistentVolumeBlock, tests.StorageClass, "VolumeSnapshot", pvc.Name)
				err = client.Create(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is restored\n", restorePvc.Name)

				restorePod = tests.GetSamplePod("restore-lvmblockpod", restorePvc.Name)
				err = client.Create(ctx, restorePod)
				Expect(err).To(BeNil())

				Eventually(func() bool {
					err := client.Get(ctx, types.NamespacedName{Name: restorePvc.Name, Namespace: restorePvc.Namespace}, restorePvc)
					return err == nil && restorePvc.Status.Phase == k8sv1.ClaimBound
				}, timeout, interval).Should(BeTrue())
				fmt.Printf("Restored PVC %s is bound\n", restorePod.Name)

				err = client.Delete(ctx, clonePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", clonePod.Name)

				err = client.Delete(ctx, clonePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Clone PVC %s is deleted\n", clonePvc.Name)

				err = client.Delete(ctx, restorePod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", restorePod.Name)

				err = client.Delete(ctx, restorePvc)
				Expect(err).To(BeNil())
				fmt.Printf("Restored Snapshot %s is deleted\n", restorePvc.Name)

				err = client.Delete(ctx, snapshot)
				Expect(err).To(BeNil())
				fmt.Printf("Snapshot %s is deleted\n", snapshot.Name)

				err = client.Delete(ctx, pod)
				Expect(err).To(BeNil())
				fmt.Printf("Pod %s is deleted\n", pod.Name)

				err = client.Delete(ctx, pvc)
				Expect(err).To(BeNil())
				fmt.Printf("PVC %s is deleted\n", pvc.Name)
			})
		})

	})
}
