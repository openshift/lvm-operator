package qe_tests

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	oteginkgo "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	LVMClusterReadyTimeout          = 5 * time.Minute
	LVMClusterForceWipeReadyTimeout = 10 * time.Minute // Extended timeout for forceWipe operations
	PodReadyTimeout                 = 3 * time.Minute
	PVCBoundTimeout                 = 3 * time.Minute
	ResourceDeleteTimeout           = 2 * time.Minute
)

var (
	tc            *TestClient
	testNamespace string
	storageClass  = "lvms-vg1"
	volumeGroup   = "vg1"
	lvmsNamespace = "openshift-lvm-storage"
)

func init() {
	klog.SetLogger(oteginkgo.GinkgoLogrFunc(g.GinkgoWriter))

	klog.LogToStderr(false)
}

func setupTest() {
	tc = NewTestClient("lvms")

	checkLvmsOperatorInstalled(tc)

	testNamespace = fmt.Sprintf("lvms-test-%d", time.Now().UnixNano())
	err := createNamespaceWithOC(testNamespace)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func cleanupLogicalVolumeByName(lvName string) {
	checkCmd := exec.Command("oc", "get", "logicalvolume", lvName, "--ignore-not-found", "-o=name")
	output, _ := checkCmd.CombinedOutput()
	if strings.TrimSpace(string(output)) == "" {
		return // Doesn't exist
	}

	patch := `{"metadata":{"finalizers":null}}`
	patchCmd := exec.Command("oc", "patch", "logicalvolume", lvName, "--type=merge", "-p", patch)
	patchCmd.CombinedOutput()

	delCmd := exec.Command("oc", "delete", "logicalvolume", lvName, "--force", "--grace-period=0", "--ignore-not-found")
	delCmd.CombinedOutput()
	logf("Cleaned up LogicalVolume: %s", lvName)
}

func int64Ptr(i int64) *int64 {
	return &i
}

var _ = g.Describe("[sig-storage] STORAGE", func() {

	g.BeforeEach(func() {
		setupTest()
	})

	g.AfterEach(func() {
		deleteSpecifiedResource("namespace", testNamespace, "")
	})

	g.It("Author:rdeore-LEVEL0-Critical-61585-[OTP][LVMS] [Filesystem] [Clone] a pvc with the same capacity should be successful", g.Label("SNO", "MNO"), func() {

		g.By("Create a PVC with the lvms csi storageclass")
		err := createPVCWithOC(pvcConfig{
			name:             "test-pvc-original",
			namespace:        testNamespace,
			storageClassName: storageClass,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create pod with the created pvc (required for WaitForFirstConsumer binding mode)")
		err = createPodWithOC(podConfig{
			name:      "test-pod-original",
			namespace: testNamespace,
			pvcName:   "test-pvc-original",
			mountPath: "/mnt/test",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PVC to be bound (happens after pod is scheduled)")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-original", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-original", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Write file to volume")
		writePodData(tc, testNamespace, "test-pod-original", "test-container", "/mnt/test")
		execCommandInPod(tc, testNamespace, "test-pod-original", "test-container", "sync")

		g.By("Create a clone pvc with the lvms storageclass")
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-clone",
			namespace:        testNamespace,
			storageClassName: storageClass,
			storage:          "1Gi",
			dataSourceName:   "test-pvc-original",
			dataSourceKind:   "PersistentVolumeClaim",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create pod with the cloned pvc (required for WaitForFirstConsumer binding mode)")
		err = createPodWithOC(podConfig{
			name:      "test-pod-clone",
			namespace: testNamespace,
			pvcName:   "test-pvc-clone",
			mountPath: "/mnt/test",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for cloned PVC to be bound (happens after pod is scheduled)")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-clone", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("Wait for cloned pod to be running")
		o.Eventually(func() corev1.PodPhase {
			pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-clone", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Delete original pvc will not impact the cloned one")
		deleteSpecifiedResource("pod", "test-pod-original", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-original", testNamespace)

		g.By("Check the cloned pod is still running")
		pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-clone", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(pod.Status.Phase).To(o.Equal(corev1.PodRunning))

		g.By("Check the file exists in cloned volume")
		checkPodDataExists(tc, testNamespace, "test-pod-clone", "test-container", "/mnt/test", true)
	})

	g.It("Author:rdeore-LEVEL0-Critical-61425-[OTP][LVMS] [Filesystem] [WaitForFirstConsumer] PVC resize on LVM cluster beyond thinpool size, but within over-provisioning limit", g.Label("SNO", "MNO"), func() {

		g.By("Get thin pool size and over provision limit")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, "thin-pool-1")

		g.By("Create a PVC with Filesystem volumeMode")
		initialCapacity := "2Gi"
		err := createPVCWithOC(pvcConfig{
			name:             "test-pvc-fs-resize",
			namespace:        testNamespace,
			storageClassName: storageClass,
			storage:          initialCapacity,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create deployment with mounted volume (WaitForFirstConsumer requires pod to exist)")
		mountPath := "/mnt/storage"
		deploymentName := "test-dep-fs-resize"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deploymentName,
			namespace: testNamespace,
			pvcName:   "test-pvc-fs-resize",
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PVC to be bound (happens after pod is scheduled)")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-fs-resize", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("Wait for deployment to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-fs-resize", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("Write data to mounted volume")
		depPods, err := tc.Clientset.CoreV1().Pods(testNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-fs-resize",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(depPods.Items)).To(o.BeNumerically(">", 0))
		podName := depPods.Items[0].Name
		writePodData(tc, testNamespace, podName, "test-container", mountPath)

		g.By("Check PVC can re-size beyond thinpool size, but within overprovisioning rate")
		targetCapacityInt64 := getRandomNum(int64(thinPoolSize+1), int64(thinPoolSize+10))
		targetCapacity := fmt.Sprintf("%dGi", targetCapacityInt64)

		g.By(fmt.Sprintf("Resize PVC from %s to %s", initialCapacity, targetCapacity))
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-fs-resize", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse(targetCapacity)
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PVC resize to complete")
		o.Eventually(func() string {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-fs-resize", metav1.GetOptions{})
			if err != nil {
				return ""
			}
			if capacity, ok := pvcObj.Status.Capacity[corev1.ResourceStorage]; ok {
				return capacity.String()
			}
			return ""
		}, LVMClusterReadyTimeout, 5*time.Second).Should(o.Equal(targetCapacity))

		g.By("Check origin data intact after resize")
		checkPodDataExists(tc, testNamespace, podName, "test-container", mountPath, true)

		g.By("Verify deployment is still healthy after resize")
		dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-fs-resize", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(dep.Status.ReadyReplicas).To(o.Equal(int32(1)))
	})

	g.It("Author:rdeore-Critical-61433-[OTP][LVMS] [Block] [WaitForFirstConsumer] PVC resize on LVM cluster beyond thinpool size, but within over-provisioning limit", g.Label("SNO", "MNO"), func() {

		g.By("Get thin pool size and over provision limit")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, "thin-pool-1")

		g.By("Create a PVC with Block volumeMode")
		initialCapacity := "2Gi"
		err := createPVCWithOC(pvcConfig{
			name:             "test-pvc-block-resize",
			namespace:        testNamespace,
			storageClassName: storageClass,
			storage:          initialCapacity,
			volumeMode:       "Block",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create deployment with block volume device (WaitForFirstConsumer requires pod to exist)")
		deploymentName := "test-dep-block"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deploymentName,
			namespace: testNamespace,
			pvcName:   "test-pvc-block-resize",
			mountPath: "/dev/dblock",
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PVC to be bound (happens after pod is scheduled)")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-block-resize", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("Wait for deployment to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-block", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("Check PVC can re-size beyond thinpool size, but within overprovisioning rate")
		targetCapacityInt64 := getRandomNum(int64(thinPoolSize+1), int64(thinPoolSize+10))
		targetCapacity := fmt.Sprintf("%dGi", targetCapacityInt64)

		g.By(fmt.Sprintf("Resize PVC from %s to %s", initialCapacity, targetCapacity))
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-block-resize", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse(targetCapacity)
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PVC resize to complete")
		o.Eventually(func() string {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-block-resize", metav1.GetOptions{})
			if err != nil {
				return ""
			}
			if capacity, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
				return capacity.String()
			}
			return ""
		}, LVMClusterReadyTimeout, 5*time.Second).Should(o.Equal(targetCapacity))

		g.By("Sync block device to ensure data is flushed")
		depPods, err := tc.Clientset.CoreV1().Pods(testNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-block",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(depPods.Items)).To(o.BeNumerically(">", 0))
		execCommandInPod(tc, testNamespace, depPods.Items[0].Name, "test-container", "sync")

		g.By("Verify deployment is still healthy after resize")
		dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-block", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(dep.Status.ReadyReplicas).To(o.Equal(int32(1)))
	})

	g.It("Author:rdeore-LEVEL0-High-66320-[OTP][LVMS] Pre-defined CSI Storageclass should get re-created automatically after deleting [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		g.By("Check lvms storageclass exists on cluster")
		_, err := tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
		if err != nil {
			g.Skip(fmt.Sprintf("Skipped: the cluster does not have storage-class: %s", storageClass))
		}

		g.By("Save the original storage class for restoration")
		originalSC, err := tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing lvms storageClass")
		err = tc.Clientset.StorageV1().StorageClasses().Delete(context.TODO(), storageClass, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			_, err := tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
			if err != nil {
				scCopy := &storagev1.StorageClass{
					ObjectMeta: metav1.ObjectMeta{
						Name:   originalSC.Name,
						Labels: originalSC.Labels,
					},
					Provisioner:          originalSC.Provisioner,
					Parameters:           originalSC.Parameters,
					ReclaimPolicy:        originalSC.ReclaimPolicy,
					AllowVolumeExpansion: originalSC.AllowVolumeExpansion,
					VolumeBindingMode:    originalSC.VolumeBindingMode,
				}
				_, err = tc.Clientset.StorageV1().StorageClasses().Create(context.TODO(), scCopy, metav1.CreateOptions{})
				if err != nil {
					logf("Warning: failed to restore storage class: %v\n", err)
				}
			}
		}()

		g.By("Check deleted lvms storageClass is re-created automatically")
		o.Eventually(func() error {
			_, err := tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
			return err
		}, 30*time.Second, 5*time.Second).Should(o.Succeed())
	})

	g.It("Author:mmakwana-High-71012-[OTP][LVMS] Verify the wiping of local volumes in LVMS [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		var (
			volumeGroup      = "vg1"
			storageClassName = "lvms-" + volumeGroup
		)

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var diskName string
		isDiskFound := false
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				diskName = disk
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}
		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		logf("Original LVMCluster JSON saved: %s\n", originLVMClusterName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		// Defer: Restore original LVMCluster from saved JSON
		defer func() {
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				logf("Restoring original LVMCluster from saved JSON...\n")
				if err := createLVMClusterFromJSON(originLVMJSON); err != nil {
					logf("Warning: Failed to restore LVMCluster from JSON: %v\n", err)
				}
			}
			if err := waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout); err != nil {
				logf("Warning: LVMCluster did not become ready: %v\n", err)
			}
		}()

		g.By("#. Create logical volume on backend disk/device")
		workerName := workerNodes[0]
		vgName := "vg-71012"
		lvName := "lv-71012"
		createLogicalVolumeOnDisk(tc, workerName, diskName, vgName, lvName)

		// Defer: Remove logical volume on disk (separate defer, runs before LVMCluster restore)
		defer func() {
			logf("Cleaning up logical volume from disk...\n")
			removeLogicalVolumeOnDisk(tc, workerName, diskName, vgName, lvName)
		}()

		g.By("#. Create a LVMCluster resource with the disk explicitly with forceWipeDevicesAndDestroyAllData")
		newLVMClusterName := "test-lvmcluster-71012"
		diskPath := "/dev/" + diskName
		err = createLVMClusterWithForceWipe(newLVMClusterName, lvmsNamespace, volumeGroup, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		// Defer: Delete new LVMCluster safely (runs before LV cleanup)
		defer func() {
			logf("Cleaning up test LVMCluster %s...\n", newLVMClusterName)
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, volumeGroup)
		}()

		g.By("#. Wait for new LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterForceWipeReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a pvc with the lvms storageclass")
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-71012",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a deployment with the created pvc")
		depName := "test-dep-71012"
		err = createDeploymentWithOC(deploymentConfig{
			name:      depName,
			namespace: testNamespace,
			pvcName:   "test-pvc-71012",
			mountPath: "/mnt/test",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			d, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71012", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return d.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment pod")
		checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, "test-dep-71012", "/mnt/test")

		g.By("#. Delete Deployment and PVC resources")
		deleteSpecifiedResource("deployment", "test-dep-71012", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-71012", testNamespace)

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, volumeGroup)

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:mmakwana-High-66241-[OTP][LVMS] Check workload management annotations are present in LVMS resources [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var diskName string
		isDiskFound := false
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				diskName = disk
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}
		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("Found LVMCluster: %s\n", originLVMClusterName)

		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("Original LVMCluster saved\n")

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		// Defer 1: Restore original LVMCluster if not exists
		defer func() {
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("#. Create a new LVMCluster resource")
		newLVMClusterName := "test-lvmcluster-66241"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName

		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		// Defer 2: Delete new LVMCluster safely (registered AFTER creation)
		defer func() {
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		g.By("#. Wait for new LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Check workload management annotations are present in LVMS resources")
		expectedSubstring := `{"effect": "PreferredDuringScheduling"}`

		deployment, err := tc.Clientset.AppsV1().Deployments(lvmsNamespace).Get(context.TODO(), "lvms-operator", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		annotation1 := deployment.Spec.Template.Annotations["target.workload.openshift.io/management"]
		logf("LVM Operator Annotations: %s\n", annotation1)
		o.Expect(annotation1).To(o.ContainSubstring(expectedSubstring))

		daemonset, err := tc.Clientset.AppsV1().DaemonSets(lvmsNamespace).Get(context.TODO(), "vg-manager", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		annotation2 := daemonset.Spec.Template.Annotations["target.workload.openshift.io/management"]
		logf("VG Manager Annotations: %s\n", annotation2)
		o.Expect(annotation2).To(o.ContainSubstring(expectedSubstring))

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:mmakwana-High-71378-[OTP][LVMS] Recover LVMS cluster from on-disk metadata [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		volumeGroup := "vg1"
		storageClassName := "lvms-" + volumeGroup

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var diskName string
		isDiskFound := false
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				diskName = disk
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}
		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("Found LVMCluster: %s\n", originLVMClusterName)

		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("Original LVMCluster saved\n")

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		// Defer 1: Restore original LVMCluster if not exists
		defer func() {
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("#. Create LVMCluster resource with single devicePath")
		newLVMClusterName := "test-lvmcluster-71378"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName

		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		// Defer 2: Delete first LVMCluster safely (registered AFTER creation)
		defer func() {
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		g.By("#. Wait for LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a PVC (pvc1)")
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-71378-1",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pvc", "test-pvc-71378-1", testNamespace)
		}()

		g.By("#. Create a deployment (dep1)")
		deployment1Name := "test-dep-71378-1"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deployment1Name,
			namespace: testNamespace,
			pvcName:   "test-pvc-71378-1",
			mountPath: "/mnt/test",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("deployment", deployment1Name, testNamespace)
		}()

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71378-1", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Fetch disk path from current LVMCluster")
		diskPaths, err := getLvmClusterPath(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		selectedDisk := strings.Fields(diskPaths)[0]
		logf("Selected Disk Path: %s\n", selectedDisk)

		g.By("#. Remove finalizers from LVMCluster, LVMVolumeGroup, and LVMVolumeGroupNodeStatus, then delete LVMCluster")
		// Matching upstream exactly: non-blocking delete, then immediately remove all finalizers
		deleteCmd := exec.Command("oc", "delete", "lvmcluster", newLVMClusterName, "-n", lvmsNamespace, "--ignore-not-found", "--wait=false")
		deleteCmdOutput, err := deleteCmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to delete lvmcluster: %s", string(deleteCmdOutput))
		removeLVMClusterFinalizers(newLVMClusterName, lvmsNamespace)
		removeLVMVolumeGroupFinalizers(deviceClassName, lvmsNamespace)
		removeLVMVolumeGroupNodeStatusFinalizers(lvmsNamespace)
		logf("LVMCluster %s deleted with all finalizers removed\n", newLVMClusterName)

		g.By("#. Create a new LVMCluster resource with same disk path (testing recovery)")
		// Use same cluster name pattern - this tests actual recovery where VG on disk is reused
		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, selectedDisk)
		o.Expect(err).NotTo(o.HaveOccurred())

		// Defer 3: Delete recovered LVMCluster safely (registered AFTER creation)
		defer func() {
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		g.By("#. Wait for recovered LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a PVC (pvc2)")
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-71378-2",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pvc", "test-pvc-71378-2", testNamespace)
		}()

		g.By("#. Create a deployment (dep2)")
		deployment2Name := "test-dep-71378-2"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deployment2Name,
			namespace: testNamespace,
			pvcName:   "test-pvc-71378-2",
			mountPath: "/mnt/test",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("deployment", deployment2Name, testNamespace)
		}()

		g.By("#. Wait for the deployment2 to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71378-2", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment2 pod")
		checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, "test-dep-71378-2", "/mnt/test")

		g.By("#. Check dep1 is still running (verifying recovery)")
		dep1, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71378-1", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(dep1.Status.ReadyReplicas).To(o.Equal(int32(1)))
		logf("The deployment %s in namespace %s is in healthy state after recovery\n", dep1.Name, dep1.Namespace)

		g.By("#. Delete Deployment and PVC resources")
		deleteSpecifiedResource("deployment", "test-dep-71378-2", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-71378-2", testNamespace)
		deleteSpecifiedResource("deployment", "test-dep-71378-1", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-71378-1", testNamespace)

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:mmakwana-High-77069-[OTP][LVMS] Make thin pool metadata size configurable in LVMS [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var diskName string
		isDiskFound := false
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				diskName = disk
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}
		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		logf("Original LVMCluster saved: %s\n", originLVMClusterName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		// Defer 1: Restore original LVMCluster if not exists
		defer func() {
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("#. Create LVMCluster resource and then patch MetadataSizeCalculationPolicy set to 'Static'")
		newLVMClusterName := "test-lvmcluster-77069"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName
		metadataSize := "100Mi"

		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		// Defer 2: Delete new LVMCluster safely (registered AFTER creation)
		defer func() {
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		err = patchMetadataSizeCalculationPolicyToStatic(newLVMClusterName, lvmsNamespace, metadataSize)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a PVC")
		storageClassName := "lvms-" + deviceClassName
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-77069",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pvc", "test-pvc-77069", testNamespace)
		}()

		g.By("#. Create a deployment")
		deploymentName := "test-dep-77069"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deploymentName,
			namespace: testNamespace,
			pvcName:   "test-pvc-77069",
			mountPath: "/mnt/test",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("deployment", deploymentName, testNamespace)
		}()

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-77069", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment pod")
		checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, "test-dep-77069", "/mnt/test")

		g.By("#. Debug into the node and check the size of the metadata for logical volumes")
		nodeName, err := getLogicalVolumeSelectedNode(testNamespace, "test-pvc-77069")
		o.Expect(err).NotTo(o.HaveOccurred())

		lvsCmd := "lvs --noheadings -o lv_name,lv_metadata_size"
		lvsOutput := execCommandInNode(tc, nodeName, lvsCmd)
		logf("Logical volume metadata size: %s\n", lvsOutput)

		expectedLvsOutput := "100.00m"
		o.Expect(lvsOutput).To(o.ContainSubstring(expectedLvsOutput))

		g.By("#. Delete Deployment and PVC resources")
		deleteSpecifiedResource("deployment", "test-dep-77069", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-77069", testNamespace)

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:rdeore-LEVEL0-High-66321-[OTP][LVMS] [Filesystem] [ext4] provision a PVC with fsType:'ext4'", g.Label("SNO", "MNO"), func() {

		mountPath := "/mnt/storage"
		uniqueSuffix := testNamespace[len(testNamespace)-10:]
		storageClassName := "lvms-ext4-" + uniqueSuffix
		provisioner := "topolvm.io"
		reclaimPolicy := corev1.PersistentVolumeReclaimDelete
		volumeBindingMode := storagev1.VolumeBindingWaitForFirstConsumer
		allowExpansion := true

		g.By("Create a new lvms storageclass with fsType:ext4")
		sc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: storageClassName,
			},
			Provisioner: provisioner,
			Parameters: map[string]string{
				"csi.storage.k8s.io/fstype": "ext4",
				"topolvm.io/device-class":   volumeGroup,
			},
			ReclaimPolicy:        &reclaimPolicy,
			AllowVolumeExpansion: &allowExpansion,
			VolumeBindingMode:    &volumeBindingMode,
		}
		_, err := tc.Clientset.StorageV1().StorageClasses().Create(context.TODO(), sc, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.StorageV1().StorageClasses().Delete(context.TODO(), storageClassName, metav1.DeleteOptions{})

		g.By("Create a pvc with the lvms storageclass")
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-66321",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "2Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pvc", "test-pvc-66321", testNamespace)
		}()

		g.By("Create deployment with the created pvc and wait for the pod ready")
		deploymentName := "test-dep-66321"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deploymentName,
			namespace: testNamespace,
			pvcName:   "test-pvc-66321",
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("deployment", deploymentName, testNamespace)
		}()

		g.By("Wait for the deployment ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-66321", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("Get deployment pods")
		pods, err := tc.Clientset.CoreV1().Pods(testNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-66321",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))

		g.By("Check the deployment's pod mounted volume fstype is ext4 by exec mount cmd in the pod")
		for _, pod := range pods.Items {
			mountCmd := fmt.Sprintf("mount | grep %s", mountPath)
			mountOutput := execCommandInPod(tc, testNamespace, pod.Name, "test-container", mountCmd)
			o.Expect(mountOutput).To(o.ContainSubstring("ext4"))
		}

		g.By("Check the deployment's pod mounted volume can be read and write")
		for _, pod := range pods.Items {
			content := fmt.Sprintf("storage test %s", getRandomString())
			randomFileName := "/testfile_" + getRandomString()
			writeCmd := fmt.Sprintf("echo '%s' > %s%s", content, mountPath, randomFileName)
			execCommandInPod(tc, testNamespace, pod.Name, "test-container", writeCmd)
			readCmd := fmt.Sprintf("cat %s%s", mountPath, randomFileName)
			readOutput := execCommandInPod(tc, testNamespace, pod.Name, "test-container", readCmd)
			o.Expect(strings.TrimSpace(readOutput)).To(o.Equal(content))
		}

		g.By("Check the deployment's pod mounted volume have the exec right")
		for _, pod := range pods.Items {
			// Create script file
			createScriptCmd := fmt.Sprintf("echo '#!/bin/bash\necho \"Hello OpenShift Storage\"' > %s/hello && chmod +x %s/hello", mountPath, mountPath)
			createOutput := execCommandInPod(tc, testNamespace, pod.Name, "test-container", createScriptCmd)
			o.Expect(createOutput).To(o.Equal(""))
			// Execute script
			execScriptCmd := fmt.Sprintf("%s/hello", mountPath)
			execOutput := execCommandInPod(tc, testNamespace, pod.Name, "test-container", execScriptCmd)
			o.Expect(execOutput).To(o.ContainSubstring("Hello OpenShift Storage"))
		}

		g.By("Check the fsType of volume mounted on the pod located node")
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-66321", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		volName := pvcObj.Spec.VolumeName

		podObj, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), pods.Items[0].Name, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		nodeName := podObj.Spec.NodeName

		checkVolumeMountCmdContain(tc, volName, nodeName, "ext4")
	})

	g.It("Author:rdeore-High-66322-[OTP][LVMS] Show status column for lvmCluster and show warning event for 'Not Enough Storage capacity' directly from PVC", g.Label("SNO", "MNO"), func() {

		thinPoolName := "thin-pool-1"
		storageClassName := "lvms-" + volumeGroup

		g.By("Wait for LVMCluster to be Ready and check status column in 'oc get' output")
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "lvmcluster", "-n", lvmsNamespace)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return ""
			}
			return string(output)
		}, LVMClusterReadyTimeout, 5*time.Second).Should(o.ContainSubstring("Ready"))

		g.By("Calculate PVC capacity exceeding overprovision limit")
		overProvisionLimit := getOverProvisionLimitByVolumeGroup(tc, volumeGroup, thinPoolName)
		pvcCapacityInt := int64(overProvisionLimit) + getRandomNum(10, 20)
		pvcCapacity := fmt.Sprintf("%dGi", pvcCapacityInt)
		logf("PVC capacity in Gi: %s (overProvisionLimit=%d)\n", pvcCapacity, overProvisionLimit)

		g.By("Create a pvc with the pre-defined lvms csi storageclass exceeding capacity")
		err := createPVCWithOC(pvcConfig{
			name:             "test-pvc-66322",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          pvcCapacity,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pvc", "test-pvc-66322", testNamespace)
		}()

		g.By("Create pod with the created pvc and check status is Pending")
		err = createPodWithOC(podConfig{
			name:      "test-pod-66322",
			namespace: testNamespace,
			pvcName:   "test-pvc-66322",
			mountPath: "/mnt/storage",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pod", "test-pod-66322", testNamespace)
		}()

		g.By("Check pod status is consistently Pending")
		o.Consistently(func() string {
			podObj, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-66322", metav1.GetOptions{})
			if err != nil {
				return ""
			}
			return string(podObj.Status.Phase)
		}, 30*time.Second, 5*time.Second).Should(o.ContainSubstring("Pending"))

		g.By("Check warning event is generated for the pvc resource")
		expectedMessage := fmt.Sprintf("Requested storage (%s) is greater than available capacity on any node", pvcCapacity)
		o.Eventually(func() bool {
			events, err := tc.Clientset.CoreV1().Events(testNamespace).List(context.TODO(), metav1.ListOptions{
				FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=PersistentVolumeClaim", "test-pvc-66322"),
			})
			if err != nil {
				return false
			}
			for _, event := range events.Items {
				if event.Reason == "NotEnoughCapacity" && strings.Contains(event.Message, expectedMessage) {
					logf("Found expected event: %s - %s\n", event.Reason, event.Message)
					return true
				}
			}
			return false
		}, 60*time.Second, 10*time.Second).Should(o.BeTrue())
	})

	g.It("Author:rdeore-High-66764-[OTP][LVMS] Show warning event for 'Removed Claim Reference' directly from PV", g.Label("SNO", "MNO"), func() {

		storageClassName := "lvms-" + volumeGroup

		g.By("Create a pvc with the pre-defined lvms csi storageclass")
		err := createPVCWithOC(pvcConfig{
			name:             "test-pvc-66764",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pvc", "test-pvc-66764", testNamespace)
		}()

		g.By("Create pod with the pvc and wait for pod to be ready")
		err = createPodWithOC(podConfig{
			name:      "test-pod-66764",
			namespace: testNamespace,
			pvcName:   "test-pvc-66764",
			mountPath: "/mnt/storage",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteSpecifiedResource("pod", "test-pod-66764", testNamespace)
		}()

		g.By("Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-66764", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Get PV name bound to PVC")
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-66764", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvName := pvcObj.Spec.VolumeName
		logf("PV name bound to PVC: %s\n", pvName)

		g.By("Remove claim reference from pv bound to pvc")
		pvPatch := `{"spec":{"claimRef": null}}`
		cmd := exec.Command("oc", "patch", "pv", pvName, "--type=merge", "-p", pvPatch)
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to patch PV: %s", string(output)))

		defer func() {
			deleteSpecifiedResource("logicalvolume", pvName, "")
		}()
		defer func() {
			deleteSpecifiedResource("pv", pvName, "")
		}()

		g.By("Check warning event is generated for the pv resource")
		expectedReason := "ClaimReferenceRemoved"
		expectedMessage := "Claim reference has been removed. This PV is no longer dynamically managed by LVM Storage and will need to be cleaned up manually"
		o.Eventually(func() bool {
			events, err := tc.Clientset.CoreV1().Events("default").List(context.TODO(), metav1.ListOptions{
				FieldSelector: fmt.Sprintf("involvedObject.name=%s", pvName),
			})
			if err != nil {
				logf("Failed to get resource %s events: %v. Trying next round.\n", pvName, err)
				return false
			}
			for _, event := range events.Items {
				if strings.Contains(event.Reason, expectedReason) && strings.Contains(event.Message, expectedMessage) {
					logf("Found expected event: %s - %s\n", event.Reason, event.Message)
					return true
				}
			}
			logf("The events of %s do not contain expected reason/message yet\n", pvName)
			return false
		}, 60*time.Second, 10*time.Second).Should(o.BeTrue())

		g.By("Delete Pod and Pvc to clean-up the pv automatically by lvms operator")
		deleteSpecifiedResource("pod", "test-pod-66764", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-66764", testNamespace)
	})

	g.It("Author:rdeore-LEVEL0-High-67001-[OTP][LVMS] Check deviceSelector logic works with combination of one valid device Path and two optionalPaths [Disruptive]", g.Label("MNO", "Serial"), func() {

		g.By("Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(freeDiskNameCountMap) < 2 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached (need at least 2)")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var mandatoryDisk string
		var optionalDisk string
		isDiskFound := false

		for diskName, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				mandatoryDisk = diskName
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}

		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		// Pick the optional disk with the highest count (most nodes have it free)
		// This avoids picking a disk that is mounted on some nodes (e.g. Prometheus EBS)
		var bestCount int64
		for diskName, count := range freeDiskNameCountMap {
			if count > bestCount {
				optionalDisk = diskName
				bestCount = count
			}
		}

		logf("Mandatory disk: /dev/%s, Optional disk: /dev/%s (free on %d nodes)\n", mandatoryDisk, optionalDisk, bestCount)

		g.By("Copy and save existing LVMCluster configuration")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		defer func() {
			g.By("Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("Create a new LVMCluster resource with paths and optionalPaths")
		newLVMClusterName := "test-lvmcluster-67001"
		deviceClassName := "vg1"
		paths := []string{"/dev/" + mandatoryDisk}
		optionalPaths := []string{"/dev/" + optionalDisk, "/dev/invalid-path"}
		err = createLVMClusterWithPathsAndOptionalPaths(newLVMClusterName, lvmsNamespace, deviceClassName, paths, optionalPaths)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		g.By("Wait for new LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Check LVMS CSI storage capacity equals backend devices/disks total size")
		pathsDiskTotalSize := getTotalDiskSizeOnAllWorkers(tc, "/dev/"+mandatoryDisk)
		optionalPathsDiskTotalSize := getTotalDiskSizeOnAllWorkers(tc, "/dev/"+optionalDisk)
		ratio, sizePercent := getOverProvisionRatioAndSizePercent("vg1")
		expectedStorageCapacity := sizePercent * (pathsDiskTotalSize + optionalPathsDiskTotalSize) / 100
		logf("EXPECTED USABLE STORAGE CAPACITY: %d Gi\n", expectedStorageCapacity)

		currentLvmStorageCapacity := getCurrentTotalLvmStorageCapacityByStorageClass("lvms-vg1")
		actualStorageCapacity := (currentLvmStorageCapacity / ratio) / 1024 // Get size in Gi
		logf("ACTUAL USABLE STORAGE CAPACITY: %d Gi\n", actualStorageCapacity)

		storageDiff := expectedStorageCapacity - actualStorageCapacity
		if storageDiff < 0 {
			storageDiff = -storageDiff
		}
		o.Expect(storageDiff < 2).To(o.BeTrue()) // there is always a difference of 1 Gi between backend disk size and usable size

		g.By("Create a pvc with the pre-set lvms csi storageclass")
		storageClassName := "lvms-" + deviceClassName
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-67001",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create pod with the created pvc")
		err = createPodWithOC(podConfig{
			name:      "test-pod-67001",
			namespace: testNamespace,
			pvcName:   "test-pvc-67001",
			mountPath: "/mnt/storage",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-67001", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Write file to volume and verify")
		writeCmd := "echo 'storage test' > /mnt/storage/testfile && cat /mnt/storage/testfile"
		output := execCommandInPod(tc, testNamespace, "test-pod-67001", "test-container", writeCmd)
		o.Expect(output).To(o.ContainSubstring("storage test"))

		g.By("Delete Pod and PVC")
		deleteSpecifiedResource("pod", "test-pod-67001", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-67001", testNamespace)

		g.By("Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

	})

	g.It("Author:rdeore-High-67002-[OTP][LVMS] Check deviceSelector logic works with only optional paths [Disruptive]", g.Label("MNO", "Serial"), func() {

		g.By("Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached (need at least 1)")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var optionalDisk string
		isDiskFound := false

		for diskName, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				optionalDisk = diskName
				isDiskFound = true
				break
			}
		}

		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		logf("Optional disk: /dev/%s\n", optionalDisk)

		g.By("Copy and save existing LVMCluster configuration")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing LVMCluster resource")
		defer func() {
			g.By("Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("Wait for old CSIStorageCapacity objects to be cleaned up")
		o.Eventually(func() int {
			cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", lvmsNamespace,
				"-o=jsonpath={.items[?(@.storageClassName==\"lvms-vg1\")].capacity}")
			output, _ := cmd.CombinedOutput()
			return len(strings.Fields(strings.TrimSpace(string(output))))
		}, 180*time.Second, 10*time.Second).Should(o.Equal(0))

		g.By("Create a new LVMCluster resource with optional paths")
		newLVMClusterName := "test-lvmcluster-67002"
		deviceClassName := "vg1"
		optionalPaths := []string{"/dev/" + optionalDisk, "/dev/invalid-path"}
		defer func() {
			g.By("Cleaning up test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()
		err = createLVMClusterWithOnlyOptionalPaths(newLVMClusterName, lvmsNamespace, deviceClassName, optionalPaths)
		o.Expect(err).NotTo(o.HaveOccurred())
		g.By("Wait for new LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Check LVMS CSI storage capacity equals backend devices/disks total size")
		optionalPathsDiskTotalSize := getTotalDiskSizeOnAllWorkers(tc, "/dev/"+optionalDisk)
		ratio, sizePercent := getOverProvisionRatioAndSizePercent("vg1")
		expectedStorageCapacity := sizePercent * optionalPathsDiskTotalSize / 100
		logf("EXPECTED USABLE STORAGE CAPACITY: %d Gi\n", expectedStorageCapacity)

		scName := "lvms-" + deviceClassName
		o.Eventually(func() int {
			capacity := getCurrentTotalLvmStorageCapacityByStorageClass(scName)
			if capacity > 0 {
				return workerNodeCount // CSI storage capacity is ready
			}
			return 0
		}, 180*time.Second, 10*time.Second).Should(o.Equal(workerNodeCount))

		currentLvmStorageCapacity := getCurrentTotalLvmStorageCapacityByStorageClass(scName)
		actualStorageCapacity := (currentLvmStorageCapacity / ratio) / 1024 // Get size in Gi
		logf("ACTUAL USABLE STORAGE CAPACITY: %d Gi\n", actualStorageCapacity)

		storageDiff := expectedStorageCapacity - actualStorageCapacity
		if storageDiff < 0 {
			storageDiff = -storageDiff
		}
		o.Expect(storageDiff < 2).To(o.BeTrue()) // there is always a difference of 1 Gi between backend disk size and usable size

		g.By("Create a pvc with the pre-set lvms csi storageclass")
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-67002",
			namespace:        testNamespace,
			storageClassName: scName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create pod with the created pvc")
		err = createPodWithOC(podConfig{
			name:      "test-pod-67002",
			namespace: testNamespace,
			pvcName:   "test-pvc-67002",
			mountPath: "/mnt/storage",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-67002", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Write file to volume and verify")
		writeCmd := "echo 'storage test' > /mnt/storage/testfile && cat /mnt/storage/testfile"
		output := execCommandInPod(tc, testNamespace, "test-pod-67002", "test-container", writeCmd)
		o.Expect(output).To(o.ContainSubstring("storage test"))

		g.By("Delete Pod and PVC")
		deleteSpecifiedResource("pod", "test-pod-67002", testNamespace)
		deleteSpecifiedResource("pvc", "test-pvc-67002", testNamespace)

		g.By("Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

	})

	g.It("Author:rdeore-High-67003-[OTP][LVMS] Check deviceSelector logic shows error when only optionalPaths are used which are invalid device paths [Disruptive]", g.Label("MNO", "Serial"), func() {

		g.By("Copy and save existing LVMCluster configuration")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("Create a new LVMCluster resource with invalid optional paths")
		newLVMClusterName := "test-lvmcluster-67003"
		deviceClassName := "vg1"
		invalidOptionalPaths := []string{"/dev/invalid-path1", "/dev/invalid-path2"}

		defer func() {
			g.By("Cleaning up test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

			deadline := time.Now().Add(2 * time.Minute)
			for time.Now().Before(deadline) {
				exists, _ := resourceExists("lvmcluster", newLVMClusterName, lvmsNamespace)
				if !exists {
					break
				}
				time.Sleep(5 * time.Second)
			}

			g.By("Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		err = createLVMClusterWithOnlyOptionalPaths(newLVMClusterName, lvmsNamespace, deviceClassName, invalidOptionalPaths)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Check LVMCluster state is 'Failed' with proper error reason")
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "lvmcluster", newLVMClusterName, "-n", lvmsNamespace, "-o=jsonpath={.status.state}")
			output, err := cmd.CombinedOutput()
			if err != nil {
				return ""
			}
			state := strings.TrimSpace(string(output))
			logf("LVMCluster %s state: %s\n", newLVMClusterName, state)
			return state
		}, 120*time.Second, 5*time.Second).Should(o.Equal("Failed"))

		errMsg := "there were no available devices to create it"
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "lvmcluster", newLVMClusterName, "-n", lvmsNamespace,
				"-o=jsonpath={.status.deviceClassStatuses[*].nodeStatus[*].reason}")
			output, err := cmd.CombinedOutput()
			if err != nil {
				return ""
			}
			errorReason := strings.TrimSpace(string(output))
			logf("LVMCluster resource error reason: %s\n", errorReason)
			return errorReason
		}, 120*time.Second, 5*time.Second).Should(o.ContainSubstring(errMsg))

		g.By("Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

	})

	g.It("Author:rdeore-High-67004-[OTP][LVMS] Check deviceSelector logic shows error when identical device path is used in both paths and optionalPaths [Disruptive]", g.Label("MNO", "Serial"), func() {

		g.By("Copy and save existing LVMCluster configuration")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		defer func() {
			g.By("Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("Create a new LVMCluster resource with same path in both paths and optionalPaths")
		newLVMClusterName := "test-lvmcluster-67004"
		deviceClassName := "vg1"
		duplicatePath := "/dev/diskpath-1"
		paths := []string{duplicatePath}
		optionalPaths := []string{duplicatePath}

		// Cleanup test LVMCluster in case webhook fails to reject (matches upstream pattern)
		defer deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

		g.By("Attempt to create LVMCluster with duplicate paths - expect error")
		err = createLVMClusterWithPathsAndOptionalPaths(newLVMClusterName, lvmsNamespace, deviceClassName, paths, optionalPaths)
		o.Expect(err).To(o.HaveOccurred())

		expectedErrMsg := fmt.Sprintf("optional device path %s is specified at multiple places in deviceClass", duplicatePath)
		o.Expect(err.Error()).To(o.ContainSubstring(expectedErrMsg))
		logf("Got expected error: %v\n", err)

	})

	g.It("Author:rdeore-LEVEL0-Critical-69191-[OTP][LVMS] [Filesystem] Support provisioning less than 1Gi size PV and re-size", g.Label("SNO", "MNO"), func() {

		storageClassName := "lvms-" + volumeGroup
		pvcName := "test-pvc-69191"
		depName := "test-dep-69191"
		mountPath := "/mnt/storage"

		g.By("Create a pvc with the lvms storageclass with size less than 1Gi")
		pvcSizeInt := getRandomNum(1, 299)
		pvcSize := fmt.Sprintf("%dMi", pvcSizeInt)
		logf("Initial PVC size: %s\n", pvcSize)

		err := createPVCWithOC(pvcConfig{
			name:             pvcName,
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          pvcSize,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcName, testNamespace)

		g.By("Create deployment with the created pvc")
		err = createDeploymentWithOC(deploymentConfig{
			name:      depName,
			namespace: testNamespace,
			pvcName:   pvcName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("deployment", depName, testNamespace)

		g.By("Wait for the deployment ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), depName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("Check PVC size is defaulted to 300Mi (minimum for xfs)")
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		actualSize := pvcObj.Status.Capacity[corev1.ResourceStorage]
		actualSizeMi := actualSize.Value() / (1024 * 1024)
		logf("Actual PVC size from status: %d Mi\n", actualSizeMi)
		o.Expect(actualSizeMi).To(o.Equal(int64(300)))

		// Get bound PV name for resize verification
		pvName := pvcObj.Spec.VolumeName
		o.Expect(pvName).NotTo(o.BeEmpty())

		g.By("Write data in pod mounted volume")
		filename, content := checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, depName, mountPath)
		logf("Wrote file %s with content %s\n", filename, content)

		g.By("Resize PVC storage capacity to a value bigger than previous value but still less than 1Gi")
		newPvcSizeInt := getRandomNum(actualSizeMi+50, actualSizeMi+700) // Range: 350-1000 Mi (matches reference)
		newPvcSize := fmt.Sprintf("%dMi", newPvcSizeInt)
		newPvcSizeQuantity := resource.MustParse(newPvcSize)
		logf("Resizing PVC from 300Mi to %s\n", newPvcSize)

		pvcObj, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = newPvcSizeQuantity
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PV resize to complete")
		waitPVVolSizeToGetResized(tc, pvName, newPvcSizeQuantity, 120*time.Second)

		g.By("Wait for PVC resize to complete")
		waitPVCResizeSuccess(tc, testNamespace, pvcName, newPvcSizeQuantity, 180*time.Second)

		g.By("Verify data integrity after first resize")
		checkDeploymentPodMountedVolumeDataExist(tc, testNamespace, depName, mountPath, filename, content)

		g.By("Write new data after first resize")
		filename, content = checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, depName, mountPath)
		logf("Wrote new file %s with content %s after first resize\n", filename, content)

		g.By("Resize PVC storage capacity to a value bigger than 1Gi")
		finalSize := "2Gi"
		finalSizeQuantity := resource.MustParse(finalSize)
		logf("Resizing PVC to %s\n", finalSize)

		pvcObj, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = finalSizeQuantity
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PV resize to 2Gi to complete")
		waitPVVolSizeToGetResized(tc, pvName, finalSizeQuantity, 120*time.Second)

		g.By("Wait for PVC resize to 2Gi to complete")
		waitPVCResizeSuccess(tc, testNamespace, pvcName, finalSizeQuantity, 180*time.Second)

		g.By("Verify data integrity after final resize")
		checkDeploymentPodMountedVolumeDataExist(tc, testNamespace, depName, mountPath, filename, content)

		g.By("Write new data after final resize")
		checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, depName, mountPath)

		logf("Successfully provisioned PVC with size less than 1Gi and resized to 2Gi with data integrity preserved\n")
	})

	g.It("Author:rdeore-LEVEL0-Critical-69753-[OTP][LVMS] [Block] Support provisioning less than 1Gi size PV and re-size", g.Label("SNO", "MNO"), func() {

		storageClassName := "lvms-" + volumeGroup
		pvcName := "test-pvc-69753"
		depName := "test-dep-69753"
		devicePath := "/dev/dblock"

		g.By("Create a pvc with the lvms storageclass with Block volumeMode and size 14Mi")
		pvcSize := "14Mi"
		pvcSizeInt := int64(14)
		logf("Initial PVC size: %s, volumeMode: Block\n", pvcSize)

		err := createPVCWithOC(pvcConfig{
			name:             pvcName,
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          pvcSize,
			volumeMode:       "Block",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcName, testNamespace)

		g.By("Create deployment with block volume device")
		err = createDeploymentWithOC(deploymentConfig{
			name:      depName,
			namespace: testNamespace,
			pvcName:   pvcName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("deployment", depName, testNamespace)

		g.By("Wait for the deployment ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), depName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		// Get PV name for resize verification
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvName := pvcObj.Spec.VolumeName
		o.Expect(pvName).NotTo(o.BeEmpty())

		g.By("Write data to block device")
		writeDeploymentDataBlockType(tc, testNamespace, depName, devicePath)

		g.By("Resize PVC storage capacity to a value bigger than previous value but still less than 1Gi")
		newPvcSizeInt := getRandomNum(pvcSizeInt+50, pvcSizeInt+1000) // Range: 64-1014 Mi (matches reference)
		newPvcSize := fmt.Sprintf("%dMi", newPvcSizeInt)
		newPvcSizeQuantity := resource.MustParse(newPvcSize)
		logf("Resizing PVC from 14Mi to %s\n", newPvcSize)

		pvcObj, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = newPvcSizeQuantity
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PV resize to complete")
		waitPVVolSizeToGetResized(tc, pvName, newPvcSizeQuantity, 120*time.Second)

		g.By("Wait for PVC resize to complete")
		waitPVCResizeSuccess(tc, testNamespace, pvcName, newPvcSizeQuantity, 180*time.Second)

		g.By("Verify data integrity after first resize")
		checkDeploymentDataBlockType(tc, testNamespace, depName, devicePath)

		g.By("Write new data after first resize")
		writeDeploymentDataBlockType(tc, testNamespace, depName, devicePath)

		g.By("Resize PVC storage capacity to a value bigger than 1Gi")
		finalSize := "2Gi"
		finalSizeQuantity := resource.MustParse(finalSize)
		logf("Resizing PVC to %s\n", finalSize)

		pvcObj, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = finalSizeQuantity
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PV resize to 2Gi to complete")
		waitPVVolSizeToGetResized(tc, pvName, finalSizeQuantity, 120*time.Second)

		g.By("Wait for PVC resize to 2Gi to complete")
		waitPVCResizeSuccess(tc, testNamespace, pvcName, finalSizeQuantity, 180*time.Second)

		g.By("Verify data integrity after final resize")
		checkDeploymentDataBlockType(tc, testNamespace, depName, devicePath)

		g.By("Write new data after final resize")
		writeDeploymentDataBlockType(tc, testNamespace, depName, devicePath)

		logf("Successfully provisioned Block PVC with size less than 1Gi and resized to 2Gi with data integrity preserved\n")
	})

	g.It("Author:rdeore-High-69611-[OTP][LVMS] Check optionalPaths work as expected with nodeSelector on multi-node OCP cluster [Disruptive]", g.Label("MNO", "Serial"), func() {

		g.By("Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		if workerNodeCount < 2 {
			g.Skip("Skipped: test case requires at least 2 worker nodes")
		}

		var optionalDisk string
		isDiskFound := false
		for diskName, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				optionalDisk = diskName
				isDiskFound = true
				break
			}
		}

		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		logf("Optional disk: /dev/%s\n", optionalDisk)

		g.By("Copy and save existing LVMCluster configuration")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("Create a new LVMCluster resource with nodeSelector and optionalPaths")
		lvmClusterObj := newLvmCluster(
			setLvmClusterName("test-lvmcluster-69611"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterDeviceClassName("vg1"),
			setLvmClusterPaths([]string{""}),
			setLvmClusterOptionalPaths([]string{"/dev/" + optionalDisk, "/dev/invalid-path"}),
		)

		defer func() {
			lvmClusterObj.deleteSafely()

			g.By("Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		err = lvmClusterObj.createWithNodeSelector("kubernetes.io/hostname", "In", []string{workerNodes[0], workerNodes[1]})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for new LVMCluster to be Ready")
		err = lvmClusterObj.waitReady(10 * time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Check LVMCluster CR definition has entry for only two worker nodes")
		cmd := exec.Command("oc", "get", "lvmcluster", lvmClusterObj.name, "-n", lvmsNamespace,
			"-o=jsonpath={.status.deviceClassStatuses[0].nodeStatus[*].node}")
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodesInUse := strings.Fields(strings.TrimSpace(string(output)))
		logf("Worker nodes in use: %v\n", workerNodesInUse)
		o.Expect(len(workerNodesInUse)).To(o.Equal(2))

		matchCount := 0
		for _, node := range workerNodesInUse {
			if node == workerNodes[0] || node == workerNodes[1] {
				matchCount++
			}
		}
		o.Expect(matchCount).To(o.Equal(2))

		g.By("Check there are exactly two pods with component name 'vg-manager' in LVMS namespace")
		vgManagerPods, err := tc.Clientset.CoreV1().Pods(lvmsNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/component=vg-manager",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("Number of vg-manager pods: %d\n", len(vgManagerPods.Items))
		o.Expect(len(vgManagerPods.Items)).To(o.Equal(2))

		g.By("Create a pvc with the pre-set lvms csi storageclass")
		storageClassName := "lvms-" + lvmClusterObj.deviceClassName
		err = createPVCWithOC(pvcConfig{
			name:             "test-pvc-69611",
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create pod with the created pvc")
		err = createPodWithOC(podConfig{
			name:      "test-pod-69611",
			namespace: testNamespace,
			pvcName:   "test-pvc-69611",
			mountPath: "/mnt/storage",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-69611", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Write file to volume and verify")
		writeCmd := "echo 'storage test 69611' > /mnt/storage/testfile && cat /mnt/storage/testfile"
		output2 := execCommandInPod(tc, testNamespace, "test-pod-69611", "test-container", writeCmd)
		o.Expect(output2).To(o.ContainSubstring("storage test 69611"))

		g.By("Delete Pod and PVC")
		tc.Clientset.CoreV1().Pods(testNamespace).Delete(context.TODO(), "test-pod-69611", metav1.DeleteOptions{})
		tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-69611", metav1.DeleteOptions{})

		g.By("Delete newly created LVMCluster resource")
		lvmClusterObj.deleteSafely()

		g.By("Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())

		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:rdeore-High-69772-[OTP][LVMS] Check LVMS operator should work with user created RAID volume as devicePath [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		pvcName := "test-pvc-69772"
		podName := "test-pod-69772"
		mountPath := "/mnt/storage"

		g.By("Check all worker nodes have at least two additional block devices/disks attached")
		freeDisksCountMap, err := getLVMSUsableDiskCountFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		if len(freeDisksCountMap) != workerNodeCount {
			g.Skip("Skipped: Cluster's worker nodes does not have minimum required free block devices/disks attached")
		}
		for _, diskCount := range freeDisksCountMap {
			if diskCount < 2 {
				g.Skip("Skipped: Cluster's worker nodes does not have minimum required two free block devices/disks attached")
			}
		}

		g.By("Copy and save existing LVMCluster configuration")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		// Defer to restore original LVMCluster (registered first, runs last)
		defer func() {
			g.By("Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		raidDiskName := "md1"
		newLVMClusterName := "test-lvmcluster-69772"
		deviceClassName := "vg1"

		g.By("Create a RAID disk on each worker node")
		for _, nodeName := range workerNodes {
			createRAIDLevel1Disk(tc, nodeName, raidDiskName)
		}

		// Defer to remove RAID disks (registered after RAID creation, runs before LVMCluster restore)
		defer func() {
			g.By("Removing RAID disks from all worker nodes")
			for _, nodeName := range workerNodes {
				raidCheck := execCommandInNode(tc, nodeName, "cat /proc/mdstat 2>/dev/null || true")
				if strings.Contains(raidCheck, raidDiskName) {
					removeRAIDLevelDisk(tc, nodeName, raidDiskName)
				}
			}
		}()

		g.By("Create a new LVMCluster resource using RAID disk as a mandatory path")
		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, "/dev/"+raidDiskName)
		o.Expect(err).NotTo(o.HaveOccurred())

		// Defer to delete new LVMCluster (registered after creation, runs before RAID removal)
		defer func() {
			g.By("Deleting test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
			// Wait for LVMCluster deletion
			deadline := time.Now().Add(2 * time.Minute)
			for time.Now().Before(deadline) {
				exists, _ := resourceExists("lvmcluster", newLVMClusterName, lvmsNamespace)
				if !exists {
					break
				}
				time.Sleep(5 * time.Second)
			}
		}()

		g.By("Wait for new LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create a pvc with the pre-set lvms csi storageclass")
		storageClassName := "lvms-" + deviceClassName
		err = createPVCWithOC(pvcConfig{
			name:             pvcName,
			namespace:        testNamespace,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcName, testNamespace)

		g.By("Create pod with the created pvc")
		err = createPodWithOC(podConfig{
			name:      podName,
			namespace: testNamespace,
			pvcName:   pvcName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podName, testNamespace)

		g.By("Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Write file to volume and verify")
		// Use "storage test" pattern matching reference checkMountedVolumeCouldRW
		writePodData(tc, testNamespace, podName, "test-container", mountPath)
		checkPodDataExists(tc, testNamespace, podName, "test-container", mountPath, true)

		g.By("#. Delete Pod and PVC")
		deleteSpecifiedResource("pod", podName, testNamespace)
		deleteSpecifiedResource("pvc", pvcName, testNamespace)

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
	})

	g.It("Author:rdeore-LEVEL0-Critical-73162-[OTP][LVMS] Check LVMCluster works with the devices configured for both thin and thick provisioning [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		storageClass1 := "lvms-vg1"
		storageClass2 := "lvms-vg2"
		volumeSnapshotClass := "lvms-vg1"

		g.By("Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(freeDiskNameCountMap) < 2 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required 2 free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var devicePaths []string
		for diskName, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				devicePaths = append(devicePaths, "/dev/"+diskName)
				if len(devicePaths) == 2 {
					break
				}
			}
		}

		if len(devicePaths) < 2 {
			g.Skip("Skipped: All Worker nodes does not have at least two free block disks/devices with same name attached")
		}

		logf("Using device paths: %v\n", devicePaths)

		g.By("Copy and save existing LVMCluster configuration")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("Create a new LVMCluster resource with two device-classes (thin + thick)")
		lvmClusterObj := newLvmCluster(
			setLvmClusterName("test-lvmcluster-73162"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterDeviceClassName("vg1"),
			setLvmClusterDeviceClassName2("vg2"),
			setLvmClusterPaths([]string{devicePaths[0], devicePaths[1]}),
		)

		defer func() {
			lvmClusterObj.deleteSafely()

			g.By("Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		err = lvmClusterObj.createWithMultiDeviceClasses()
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for new LVMCluster to be Ready")
		err = lvmClusterObj.waitReady(10 * time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Check two lvms preset storage-classes are present - one for each volumeGroup")
		_, err = tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass1, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("StorageClass %s exists\n", storageClass1)

		_, err = tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass2, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("StorageClass %s exists\n", storageClass2)

		g.By("Check only one preset lvms volumeSnapshotClass is present (for volumeGroup with thinPoolConfig)")
		vscExists, _ := resourceExists("volumesnapshotclass", volumeSnapshotClass, "")
		o.Expect(vscExists).To(o.BeTrue())
		logf("VolumeSnapshotClass %s exists (for thin provisioning)\n", volumeSnapshotClass)

		vsc2Exists, _ := resourceExists("volumesnapshotclass", "lvms-vg2", "")
		o.Expect(vsc2Exists).To(o.BeFalse())
		logf("VolumeSnapshotClass lvms-vg2 does not exist (thick provisioning doesn't support snapshots)\n")

		g.By("Check available storage capacity of thick provisioning SC equals backend disk size")
		thickCapacityMiB := getCurrentTotalLvmStorageCapacityByStorageClass(storageClass2)
		thickCapacityGiB := thickCapacityMiB / 1024
		logf("Thick provisioning storage capacity: %d GiB\n", thickCapacityGiB)

		backendDiskSizeGiB := getTotalDiskSizeOnAllWorkers(tc, devicePaths[1])
		logf("Backend disk size: %d GiB\n", backendDiskSizeGiB)

		diff := thickCapacityGiB - backendDiskSizeGiB
		if diff < 0 {
			diff = -diff
		}
		o.Expect(diff < 2).To(o.BeTrue())

		pvc1Name := "test-pvc-73162-thin"
		dep1Name := "test-dep-73162-thin"
		pvc2Name := "test-pvc-73162-thick"
		dep2Name := "test-dep-73162-thick"
		mountPath := "/mnt/storage"

		g.By("Create PVC-1 with thin provisioning storage class")
		err = createPVCWithOC(pvcConfig{
			name:             pvc1Name,
			namespace:        testNamespace,
			storageClassName: storageClass1,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvc1Name, testNamespace)

		g.By("Create Deployment-1 with thin provisioned PVC")
		err = createDeploymentWithOC(deploymentConfig{
			name:      dep1Name,
			namespace: testNamespace,
			pvcName:   pvc1Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("deployment", dep1Name, testNamespace)

		g.By("Wait for Deployment-1 to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), dep1Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("Write data to thin provisioned volume")
		checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, dep1Name, mountPath)

		g.By("Create PVC-2 with thick provisioning storage class")
		err = createPVCWithOC(pvcConfig{
			name:             pvc2Name,
			namespace:        testNamespace,
			storageClassName: storageClass2,
			storage:          "20Mi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvc2Name, testNamespace)

		g.By("Create Deployment-2 with thick provisioned PVC")
		err = createDeploymentWithOC(deploymentConfig{
			name:      dep2Name,
			namespace: testNamespace,
			pvcName:   pvc2Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("deployment", dep2Name, testNamespace)

		g.By("Wait for Deployment-2 to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), dep2Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("Write data to thick provisioned volume")
		filename2, content2 := checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, dep2Name, mountPath)

		// Get PV name for resize verification
		pvc2Obj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), pvc2Name, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pv2Name := pvc2Obj.Spec.VolumeName
		o.Expect(pv2Name).NotTo(o.BeEmpty())

		g.By("Resize thick provisioned PVC to 2Gi")
		resizeQuantity := resource.MustParse("2Gi")
		pvc2Obj.Spec.Resources.Requests[corev1.ResourceStorage] = resizeQuantity
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvc2Obj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PV resize to complete")
		waitPVVolSizeToGetResized(tc, pv2Name, resizeQuantity, 120*time.Second)

		g.By("Wait for thick PVC resize to complete")
		waitPVCResizeSuccess(tc, testNamespace, pvc2Name, resizeQuantity, 180*time.Second)

		g.By("Verify data integrity after resize")
		checkDeploymentPodMountedVolumeDataExist(tc, testNamespace, dep2Name, mountPath, filename2, content2)

		g.By("Write new data after resize")
		checkDeploymentPodMountedVolumeCouldRW(tc, testNamespace, dep2Name, mountPath)

		g.By("Delete Deployments and PVCs")
		deleteSpecifiedResource("deployment", dep1Name, testNamespace)
		deleteSpecifiedResource("pvc", pvc1Name, testNamespace)
		deleteSpecifiedResource("deployment", dep2Name, testNamespace)
		deleteSpecifiedResource("pvc", pvc2Name, testNamespace)

		g.By("Delete newly created LVMCluster resource")
		lvmClusterObj.deleteSafely()

		g.By("Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())

		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:rdeore-LEVEL0-Critical-61863-[OTP][LVMS] [Filesystem] [Snapshot] should restore volume with snapshot dataSource successfully and the volume could be read and written", g.Label("SNO", "MNO"), func() {

		volumeGroup := "vg1"
		storageClassName := "lvms-" + volumeGroup
		volumeSnapshotClassName := "lvms-" + volumeGroup
		mountPath := "/mnt/test"

		g.By("#. Create new namespace for the test scenario")
		testNs := fmt.Sprintf("lvms-test-61863-%d", time.Now().UnixNano())
		err := createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Create a PVC with the lvms csi storageclass")
		pvcOriName := "test-pvc-snap-ori"
		err = createPVCWithOC(pvcConfig{
			name:             pvcOriName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "2Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcOriName, testNs)

		g.By("#. Create pod with the created pvc and wait for the pod ready")
		podOriName := "test-pod-snap-ori"
		err = createPodWithOC(podConfig{
			name:      podOriName,
			namespace: testNs,
			pvcName:   pvcOriName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podOriName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Write data to volume")
		writePodData(tc, testNs, podOriName, "test-container", mountPath)
		execCommandInPod(tc, testNs, podOriName, "test-container", "sync")

		g.By("#. Create volumesnapshot and wait for ready_to_use")
		snapshotName := "test-snapshot-61863"
		snapshotYAML := fmt.Sprintf(`apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: %s
  namespace: %s
spec:
  volumeSnapshotClassName: %s
  source:
    persistentVolumeClaimName: %s
`, snapshotName, testNs, volumeSnapshotClassName, pvcOriName)

		cmd := exec.Command("oc", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(snapshotYAML)
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to create snapshot: %s", string(output)))
		defer func() {
			exec.Command("oc", "delete", "volumesnapshot", snapshotName, "-n", testNs, "--ignore-not-found").Run()
		}()

		g.By("#. Wait for volumesnapshot to be ready")
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "volumesnapshot", snapshotName, "-n", testNs, "-o=jsonpath={.status.readyToUse}")
			output, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(output))
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal("true"))

		g.By("#. Create a restored pvc with snapshot dataSource")
		pvcRestoreName := "test-pvc-snap-restore"
		err = createPVCWithOC(pvcConfig{
			name:             pvcRestoreName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "2Gi",
			dataSourceName:   snapshotName,
			dataSourceKind:   "VolumeSnapshot",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcRestoreName, testNs)

		g.By("#. Create pod with the restored pvc and wait for the pod ready")
		podRestoreName := "test-pod-snap-restore"
		err = createPodWithOC(podConfig{
			name:      podRestoreName,
			namespace: testNs,
			pvcName:   pvcRestoreName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podRestoreName, testNs)

		g.By("#. Wait for restored PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for restore pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check the data exists in restored volume")
		checkPodDataExists(tc, testNs, podRestoreName, "test-container", mountPath, true)

		g.By("#. Check original pod and restored pod are deployed on same worker node")
		podOriObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		podRestoreObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(podOriObj.Spec.NodeName).To(o.Equal(podRestoreObj.Spec.NodeName))
	})

	g.It("Author:rdeore-LEVEL0-Critical-61894-[OTP][LVMS] [Block] [Snapshot] should restore volume with snapshot dataSource successfully and the volume could be read and written", g.Label("SNO", "MNO"), func() {

		volumeGroup := "vg1"
		storageClassName := "lvms-" + volumeGroup
		volumeSnapshotClassName := "lvms-" + volumeGroup
		devicePath := "/dev/dblock"

		g.By("#. Create new namespace for the test scenario")
		testNs := fmt.Sprintf("lvms-test-61894-%d", time.Now().UnixNano())
		err := createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Create a PVC with Block volumeMode")
		pvcOriName := "test-pvc-block-snap-ori"
		err = createPVCWithOC(pvcConfig{
			name:             pvcOriName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "2Gi",
			volumeMode:       "Block",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcOriName, testNs)

		g.By("#. Create pod with the created pvc (using volumeDevices for block mode)")
		podOriName := "test-pod-block-snap-ori"
		err = createPodWithOC(podConfig{
			name:      podOriName,
			namespace: testNs,
			pvcName:   pvcOriName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podOriName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Write data to raw block volume")
		writeDataIntoRawBlockVolume(tc, testNs, podOriName, "test-container", devicePath)
		execCommandInPod(tc, testNs, podOriName, "test-container", "sync")

		g.By("#. Create volumesnapshot and wait for ready_to_use")
		snapshotName := "test-snapshot-61894"
		snapshotYAML := fmt.Sprintf(`apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: %s
  namespace: %s
spec:
  volumeSnapshotClassName: %s
  source:
    persistentVolumeClaimName: %s
`, snapshotName, testNs, volumeSnapshotClassName, pvcOriName)

		cmd := exec.Command("oc", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(snapshotYAML)
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to create snapshot: %s", string(output)))
		defer func() {
			exec.Command("oc", "delete", "volumesnapshot", snapshotName, "-n", testNs, "--ignore-not-found").Run()
		}()

		g.By("#. Wait for volumesnapshot to be ready")
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "volumesnapshot", snapshotName, "-n", testNs, "-o=jsonpath={.status.readyToUse}")
			output, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(output))
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal("true"))

		g.By("#. Create a restored pvc with Block volumeMode and snapshot dataSource")
		pvcRestoreName := "test-pvc-block-snap-restore"
		err = createPVCWithOC(pvcConfig{
			name:             pvcRestoreName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "2Gi",
			volumeMode:       "Block",
			dataSourceName:   snapshotName,
			dataSourceKind:   "VolumeSnapshot",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcRestoreName, testNs)

		g.By("#. Create pod with the restored pvc (using volumeDevices for block mode)")
		podRestoreName := "test-pod-block-snap-restore"
		err = createPodWithOC(podConfig{
			name:      podRestoreName,
			namespace: testNs,
			pvcName:   pvcRestoreName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podRestoreName, testNs)

		g.By("#. Wait for restored PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for restore pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check the data exists in restored volume")
		checkDataInRawBlockVolume(tc, testNs, podRestoreName, "test-container", devicePath)

		g.By("#. Check original pod and restored pod are deployed on same worker node")
		podOriObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		podRestoreObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(podOriObj.Spec.NodeName).To(o.Equal(podRestoreObj.Spec.NodeName))
	})

	g.It("Author:rdeore-Critical-61998-[OTP][LVMS] [Block] [Snapshot] should restore volume larger than disk size with snapshot dataSource successfully and the volume could be read and written [Serial]", g.Label("SNO", "MNO", "Serial"), func() {

		var (
			storageClassName        = "lvms-" + volumeGroup
			volumeSnapshotClassName = "lvms-" + volumeGroup
			thinPoolName            = "thin-pool-1"
			devicePath              = "/dev/dblock"
			pvcOriName              = "test-pvc-block-snap-ori"
			pvcRestoreName          = "test-pvc-block-snap-restore"
		)

		g.By("#. Create new namespace for the test scenario")
		testNs := "test-61998-" + fmt.Sprintf("%d", time.Now().Unix())
		err := createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Get thin pool size and calculate capacity bigger than disk size")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, thinPoolName)
		pvcCapacity := strconv.FormatInt(int64(thinPoolSize)+getRandomNum(2, 10), 10) + "Gi"
		logf("Using PVC capacity %s (thin pool size: %d Gi)\n", pvcCapacity, thinPoolSize)

		g.By("#. Create a PVC with Block volumeMode and capacity bigger than disk size")
		err = createPVCWithOC(pvcConfig{
			name:             pvcOriName,
			namespace:        testNs,
			storageClassName: storageClassName,
			volumeMode:       "Block",
			storage:          pvcCapacity,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcOriName, testNs)

		g.By("#. Create pod with the created pvc and wait for the pod ready")
		podOriName := "test-pod-block-snap-ori"
		err = createPodWithOC(podConfig{
			name:      podOriName,
			namespace: testNs,
			pvcName:   pvcOriName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podOriName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcOriName, testNs, thinPoolSize)

		g.By("#. Write data to raw block volume")
		writeDataIntoRawBlockVolume(tc, testNs, podOriName, "test-container", devicePath)

		g.By("#. Sync data to disk")
		execCommandInPod(tc, testNs, podOriName, "test-container", "sync")

		g.By("#. Create volumesnapshot")
		snapshotName := "test-snapshot-61998"
		snapshotYAML := fmt.Sprintf(`apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: %s
  namespace: %s
spec:
  volumeSnapshotClassName: %s
  source:
    persistentVolumeClaimName: %s
`, snapshotName, testNs, volumeSnapshotClassName, pvcOriName)

		cmd := exec.Command("oc", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(snapshotYAML)
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to create snapshot: %s", string(output)))
		defer func() {
			exec.Command("oc", "delete", "volumesnapshot", snapshotName, "-n", testNs, "--ignore-not-found").Run()
		}()

		g.By("#. Wait for volumesnapshot to be ready")
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "volumesnapshot", snapshotName, "-n", testNs, "-o=jsonpath={.status.readyToUse}")
			output, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(output))
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal("true"))

		g.By("#. Create a restored pvc with snapshot dataSource")
		err = createPVCWithOC(pvcConfig{
			name:             pvcRestoreName,
			namespace:        testNs,
			storageClassName: storageClassName,
			volumeMode:       "Block",
			storage:          pvcCapacity,
			dataSourceName:   snapshotName,
			dataSourceKind:   "VolumeSnapshot",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcRestoreName, testNs)

		g.By("#. Create pod with the restored pvc and wait for the pod ready")
		podRestoreName := "test-pod-block-snap-restore"
		err = createPodWithOC(podConfig{
			name:      podRestoreName,
			namespace: testNs,
			pvcName:   pvcRestoreName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podRestoreName, testNs)

		g.By("#. Wait for restored PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for restored pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check restored volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcRestoreName, testNs, thinPoolSize)

		g.By("#. Check the data exists in restored volume")
		checkDataInRawBlockVolume(tc, testNs, podRestoreName, "test-container", devicePath)

		g.By("#. Check original pod and restored pod are deployed on same worker node")
		podOriObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		podRestoreObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(podOriObj.Spec.NodeName).To(o.Equal(podRestoreObj.Spec.NodeName))
	})

	g.It("Author:rdeore-High-73363-[OTP][LVMS] Check hot reload of lvmd configuration is working [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		var (
			storageCapacity    int
			lvmdConfigFilePath = "/etc/topolvm/lvmd.yaml"
			modifyLvmdCmd      = `sed -ri 's/^(\s*)(spare-gb\s*:\s*0\s*$)/\1spare-gb: 10/' /etc/topolvm/lvmd.yaml; mv /etc/topolvm/lvmd.yaml /etc/topolvm/tmp-73363.yaml; cat /etc/topolvm/tmp-73363.yaml >> /etc/topolvm/lvmd.yaml`
		)

		devicePaths, err := getLvmClusterPaths(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(devicePaths) == 0 {
			g.Skip("Skipped: No device paths found in current LVMCluster")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNode := workerNodes[0]

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLvmClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLvmCluster := newLvmCluster(
			setLvmClusterName(originLvmClusterName),
			setLvmClusterNamespace(lvmsNamespace),
		)
		originLVMJSON, err := getLVMClusterJSON(originLvmCluster.name, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("Original LVMCluster JSON saved\n")
		originStorageCapacity := originLvmCluster.getCurrentTotalLvmStorageCapacityByWorkerNode(workerNode)

		g.By("#. Delete existing LVMCluster resource")
		// Normal delete matching upstream: no finalizer bypass, if it hangs the test fails
		deleteSpecifiedResource("lvmcluster", originLvmCluster.name, lvmsNamespace)

		g.By("#. Create a new LVMCluster resource without thin-pool deviceClass, as 'spare-gb' is only applicable to thick provisioning")
		// Upstream template uses only paths[0] (single path) for thick provisioning
		lvmClusterObj := newLvmCluster(
			setLvmClusterName("test-lvmcluster-73363"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterDeviceClassName("vg1"),
			setLvmClusterPaths([]string{devicePaths[0]}),
		)
		err = lvmClusterObj.createWithoutThinPool()
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			lvmClusterObj.deleteSafely()

			// Wait for CSIStorageCapacity objects to be cleaned up before restoring
			storageClassName := "lvms-" + lvmClusterObj.deviceClassName
			for i := 0; i < 18; i++ {
				cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", lvmsNamespace,
					"-o=jsonpath={.items[?(@.storageClassName==\""+storageClassName+"\")].capacity}")
				output, _ := cmd.CombinedOutput()
				if len(strings.Fields(strings.TrimSpace(string(output)))) == 0 {
					break
				}
				time.Sleep(10 * time.Second)
			}

			exists, _ := resourceExists("lvmcluster", originLvmCluster.name, lvmsNamespace)
			if !exists {
				logf("Cleanup: Restoring original LVMCluster %s...\n", originLvmCluster.name)
				if err := originLvmCluster.createWithExportJSON(originLVMJSON); err != nil {
					logf("Warning: Failed to restore original LVMCluster: %v\n", err)
				}
			}
			_ = originLvmCluster.waitReady(5 * time.Minute)
		}()

		g.By("#. Wait for new LVMCluster to be Ready")
		err = lvmClusterObj.waitReady(10 * time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())
		checkLVMClusterAndVGManagerPodReady(tc, lvmClusterObj)

		g.By("#. Get new CSIStorageCapacity object capacity value from one of the worker nodes")
		o.Eventually(func() int {
			storageCapacity = lvmClusterObj.getCurrentTotalLvmStorageCapacityByWorkerNode(workerNode)
			return storageCapacity
		}, PVCBoundTimeout, 5*time.Second).ShouldNot(o.Equal(originStorageCapacity))

		g.By("#. Update lvmd.config file from the worker node")
		output := execCommandInNode(tc, workerNode, modifyLvmdCmd)
		logf("Modified lvmd.yaml output: %s\n", output)

		g.By("#. Check 'vg-manager' resource pod status is 'Running' and LVMCluster state is 'Ready'")
		checkLVMClusterAndVGManagerPodReady(tc, lvmClusterObj)

		g.By("#. Check CSIStorageCapacity object capacity value is updated as per the new 'spare-gb' value")
		o.Eventually(func() int {
			newStorageCapacity := lvmClusterObj.getCurrentTotalLvmStorageCapacityByWorkerNode(workerNode)
			return newStorageCapacity
		}, ResourceDeleteTimeout, 5*time.Second).Should(o.Equal(storageCapacity - 10240)) // Subtracting 10Gi equivalent (10240 MiB)

		g.By("#. Remove new config files from worker node - lvmd.yaml will be auto-regenerated via hot reload")
		cleanupCmd := "rm -rf /etc/topolvm/tmp-73363.yaml " + lvmdConfigFilePath
		execCommandInNode(tc, workerNode, cleanupCmd)

		g.By("#. Check 'vg-manager' resource pod status is 'Running' and LVMCluster state is 'Ready'")
		checkLVMClusterAndVGManagerPodReady(tc, lvmClusterObj)

		g.By("#. Check CSIStorageCapacity object capacity value is updated back to original value")
		o.Eventually(func() int {
			newStorageCapacity := lvmClusterObj.getCurrentTotalLvmStorageCapacityByWorkerNode(workerNode)
			return newStorageCapacity
		}, ResourceDeleteTimeout, 5*time.Second).Should(o.Equal(storageCapacity))
	})

	g.It("Author:rdeore-High-73540-[OTP][LVMS] [Resize] [Thick] Enable LVMCluster configurations without thinPoolConfig [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		devicePaths, err := getLvmClusterPaths(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(devicePaths)).To(o.BeNumerically(">", 0))
		logf("Device paths from original LVMCluster: paths=%v\n", devicePaths)

		originLvmCluster := newLvmCluster(setLvmClusterName(originLVMClusterName), setLvmClusterNamespace(lvmsNamespace))

		// Register cleanup BEFORE any destructive operations to ensure LVMCluster is restored even if test fails
		defer func() {
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				originLvmCluster.createWithExportJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("#. Wait for old CSIStorageCapacity objects to be cleaned up")
		o.Eventually(func() int {
			cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", lvmsNamespace,
				"-o=jsonpath={.items[?(@.storageClassName==\""+storageClass+"\")].capacity}")
			output, _ := cmd.CombinedOutput()
			return len(strings.Fields(strings.TrimSpace(string(output))))
		}, 180*time.Second, 10*time.Second).Should(o.Equal(0))

		g.By("#. Create a new LVMCluster resource without thin-pool device")
		// Upstream template uses only paths[0] (single path) for thick provisioning
		lvmCluster := newLvmCluster(
			setLvmClusterName("test-lvmcluster-73540"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterPaths([]string{devicePaths[0]}),
		)
		err = lvmCluster.createWithoutThinPool()
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			logf("Cleanup: Deleting test LVMCluster %s...\n", lvmCluster.name)
			lvmCluster.deleteSafely()
		}()

		err = lvmCluster.waitReady(10 * time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a new project/namespace for the scenario")
		testNs := "test-73540-" + fmt.Sprintf("%d", time.Now().Unix())
		err = createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Check there is no lvms volumeSnapshotClass resource is present")
		// For thick provisioning, volumeSnapshotClass should NOT be created (matching upstream)
		vscCmd := exec.Command("oc", "get", "volumesnapshotclass", "lvms-"+volumeGroup, "--ignore-not-found", "-o=jsonpath={.metadata.name}")
		vscOutput, _ := vscCmd.CombinedOutput()
		o.Expect(strings.TrimSpace(string(vscOutput))).To(o.BeEmpty(), "VolumeSnapshotClass lvms-%s should not exist for thick-provisioned LVMCluster", volumeGroup)

		g.By("#. Check available storage capacity of preset lvms SC (thick provisioning) equals to the backend total disks size")
		// Reference uses only devicePaths[0] for capacity comparison
		pathsDiskTotalSize := getTotalDiskSizeOnAllWorkers(tc, devicePaths[0])
		logf("BACKEND DISK SIZE: %d GiB\n", pathsDiskTotalSize)
		// Wait for CSIStorageCapacity to converge to thick-provisioned values (no overprovision)
		o.Eventually(func() bool {
			thickProvisioningStorageCapacity := getCurrentTotalLvmStorageCapacityByStorageClass(storageClass) / 1024
			logf("ACTUAL USABLE STORAGE CAPACITY: %d GiB\n", thickProvisioningStorageCapacity)
			storageDiff := float64(thickProvisioningStorageCapacity - pathsDiskTotalSize)
			absDiff := math.Abs(storageDiff)
			return int(absDiff) < 2
		}, 180*time.Second, 10*time.Second).Should(o.BeTrue(), "thick-provisioned capacity should be within 2 GiB of backend disk size %d GiB", pathsDiskTotalSize)

		g.By("#. Create a pvc with the preset lvms csi storageclass with thick provisioning")
		pvcName := "test-pvc-73540"
		err = createPVCWithOC(pvcConfig{
			name:             pvcName,
			namespace:        testNs,
			storageClassName: storageClass,
			storage:          "20Mi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcName, testNs)

		g.By("#. Create a deployment with the created pvc and wait for the pod ready")
		mountPath := "/mnt/storage"
		deploymentName := "test-dep-73540"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deploymentName,
			namespace: testNs,
			pvcName:   pvcName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("deployment", deploymentName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for deployment to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Check thick provisioned logical volume (LV) has expected attribute in the backend node")
		nodeName, err := getLogicalVolumeSelectedNode(testNs, pvcName)
		o.Expect(err).NotTo(o.HaveOccurred())
		lvOutput := execCommandInNode(tc, nodeName, "lvs --noheadings -o lv_attr,vg_name | grep vg1")
		o.Expect(lvOutput).To(o.ContainSubstring("-wi-")) // `-wi-` attribute indicates a thick-provisioned logical volume

		g.By("#. Write a file to volume and verify read-back")
		// Reference: dep.checkPodMountedVolumeCouldRW(oc) - writes and reads back
		pods, err := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-73540",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podName := pods.Items[0].Name

		// Write data and verify read-back (matching reference checkPodMountedVolumeCouldRW behavior)
		testContent := fmt.Sprintf("storage test %d", time.Now().UnixNano())
		testFileName := fmt.Sprintf("/testfile_%d", time.Now().UnixNano())
		writeCmd := fmt.Sprintf("echo '%s' > %s%s", testContent, mountPath, testFileName)
		execCommandInPod(tc, testNs, podName, "test-container", writeCmd)

		// Read back and verify
		readCmd := fmt.Sprintf("cat %s%s", mountPath, testFileName)
		readOutput := execCommandInPod(tc, testNs, podName, "test-container", readCmd)
		o.Expect(strings.TrimSpace(readOutput)).To(o.Equal(testContent))
		logf("Write/Read verification passed\n")

		g.By("#. Resize pvc storage capacity to a value bigger than 1Gi")
		expandedCapacity := "2Gi"
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse(expandedCapacity)
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for PVC resize to complete")
		o.Eventually(func() string {
			pvcUpdated, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcName, metav1.GetOptions{})
			if err != nil {
				return ""
			}
			if capacity, ok := pvcUpdated.Status.Capacity[corev1.ResourceStorage]; ok {
				return capacity.String()
			}
			return ""
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(expandedCapacity))

		g.By("#. Check origin data intact and write new data in pod")
		// Reference: resizeAndCheckDataIntegrity checks data exists AND writes new data after resize
		// Verify original data still exists
		readAfterResize := execCommandInPod(tc, testNs, podName, "test-container", readCmd)
		o.Expect(strings.TrimSpace(readAfterResize)).To(o.Equal(testContent))
		logf("Original data intact after resize\n")

		// Write new data after resize to verify volume is still writable
		newTestContent := fmt.Sprintf("post-resize test %d", time.Now().UnixNano())
		newTestFileName := fmt.Sprintf("/testfile_post_resize_%d", time.Now().UnixNano())
		newWriteCmd := fmt.Sprintf("echo '%s' > %s%s", newTestContent, mountPath, newTestFileName)
		execCommandInPod(tc, testNs, podName, "test-container", newWriteCmd)

		// Read back new data
		newReadCmd := fmt.Sprintf("cat %s%s", mountPath, newTestFileName)
		newReadOutput := execCommandInPod(tc, testNs, podName, "test-container", newReadCmd)
		o.Expect(strings.TrimSpace(newReadOutput)).To(o.Equal(newTestContent))
		logf("Post-resize write/read verification passed\n")

		g.By("#. Delete Deployment and PVC")
		// Matching upstream: simple delete of deployment and PVC
		deleteSpecifiedResource("deployment", deploymentName, testNs)
		deleteSpecifiedResource("pvc", pvcName, testNs)

		g.By("#. Delete newly created LVMCluster resource")
		lvmCluster.deleteSafely()

		g.By("#. Create original LVMCluster resource")
		err = originLvmCluster.createWithExportJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:mmakwana-High-73566-[OTP][LVMS] Verify support using encrypted devices [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}
		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)
		var diskName string
		isDiskFound := false
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				diskName = disk
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}
		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLvmCluster := newLvmCluster(setLvmClusterName(originLVMClusterName), setLvmClusterNamespace(lvmsNamespace))

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("#. Set an encryption passphrase for the disk")
		passphrase := "encrypted"
		setDiskEncryptPassphrase(tc, diskName, passphrase, workerNodes)

		g.By("#. Create LVMCluster resource with encrypted device")
		lvmCluster := newLvmCluster(
			setLvmClusterName("test-lvmcluster-73566"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterPaths([]string{"/dev/mapper/encrypted"}),
			setLvmClusterOptionalPaths([]string{"/dev/diskpath-2", "/dev/diskpath-3"}),
		)
		err = lvmCluster.create()
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			lvmCluster.deleteSafely()

			deadline := time.Now().Add(3 * time.Minute)
			for time.Now().Before(deadline) {
				cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", "openshift-lvm-storage",
					"-o=jsonpath={.items[?(@.storageClassName==\""+storageClass+"\")].capacity}")
				output, _ := cmd.CombinedOutput()
				if len(strings.Fields(strings.TrimSpace(string(output)))) == 0 {
					break
				}
				time.Sleep(10 * time.Second)
			}

			g.By("#. Wipe an encryption passphrase from the disk")
			wipeDiskEncryptPassphrase(tc, diskName, workerNodes)

			g.By("#. Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				originLvmCluster.createWithExportJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		err = lvmCluster.waitReady(10 * time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create new project/namespace for the scenario")
		testNs := "test-73566-" + fmt.Sprintf("%d", time.Now().Unix())
		err = createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), "test-dep-73566", metav1.DeleteOptions{
				GracePeriodSeconds: int64Ptr(0),
			})
			deadline := time.Now().Add(2 * time.Minute)
			for time.Now().Before(deadline) {
				pods, _ := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
					LabelSelector: "app=test-dep-73566",
				})
				if len(pods.Items) == 0 {
					break
				}
				time.Sleep(5 * time.Second)
			}
			tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), "test-pvc-73566", metav1.DeleteOptions{})
			time.Sleep(5 * time.Second)
			tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})
		}()

		g.By("#. Create a PVC with the preset lvms storageclass")
		pvcName := "test-pvc-73566"
		err = createPVCWithOC(pvcConfig{
			name:             pvcName,
			namespace:        testNs,
			storageClassName: storageClass,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcName, testNs)

		g.By("#. Create a deployment with the created pvc")
		mountPath := "/mnt/storage"
		deploymentName := "test-dep-73566"
		err = createDeploymentWithOC(deploymentConfig{
			name:      deploymentName,
			namespace: testNs,
			pvcName:   pvcName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), deploymentName, metav1.DeleteOptions{})
			deadline := time.Now().Add(2 * time.Minute)
			for time.Now().Before(deadline) {
				pods, _ := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
					LabelSelector: "app=test-dep-73566",
				})
				if len(pods.Items) == 0 {
					break
				}
				time.Sleep(5 * time.Second)
			}
		}()

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment pod")
		pods, err := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-73566",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podName := pods.Items[0].Name
		writePodData(tc, testNs, podName, "test-container", mountPath)

		g.By("#. Verify data can be read back")
		checkPodDataExists(tc, testNs, podName, "test-container", mountPath, true)

		g.By("#. Delete Deployment and wait for pods to fully terminate")
		err = tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), deploymentName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		o.Eventually(func() bool {
			_, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
			return err != nil
		}, ResourceDeleteTimeout, 5*time.Second).Should(o.BeTrue())

		o.Eventually(func() int {
			pods, _ := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
				LabelSelector: "app=test-dep-73566",
			})
			return len(pods.Items)
		}, ResourceDeleteTimeout, 5*time.Second).Should(o.Equal(0))

		g.By("#. Delete PVC after pods are terminated")
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete newly created LVMCluster resource")
		lvmCluster.deleteSafely()

		g.By("#. Wait for disk to be released after LVMCluster deletion")
		time.Sleep(30 * time.Second) // Give time for LVM to fully release the disk

		g.By("#. Wait for old CSIStorageCapacity objects to be cleaned up")
		o.Eventually(func() int {
			cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", "openshift-lvm-storage",
				"-o=jsonpath={.items[?(@.storageClassName==\""+storageClass+"\")].capacity}")
			output, _ := cmd.CombinedOutput()
			return len(strings.Fields(strings.TrimSpace(string(output))))
		}, 180*time.Second, 10*time.Second).Should(o.Equal(0))

		g.By("#. Wipe encryption from the disk")
		wipeDiskEncryptPassphrase(tc, diskName, workerNodes)

		g.By("#. Wait for disk to be fully released after encryption wipe")
		time.Sleep(10 * time.Second)

		g.By("#. Create original LVMCluster resource")
		err = originLvmCluster.createWithExportJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:rdeore-High-60835-[OTP][LVMS] Support multiple storage classes on single lvms deployment [Disruptive]", g.Label("MNO", "Serial"), func() {

		var (
			storageClass1 = "lvms-vg1"
			storageClass2 = "lvms-vg2"
		)

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(freeDiskNameCountMap) < 2 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}
		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)
		var devicePaths []string
		for diskName, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				devicePaths = append(devicePaths, "/dev/"+diskName)
				delete(freeDiskNameCountMap, diskName)
				if len(devicePaths) == 2 {
					break
				}
			}
		}
		if len(devicePaths) < 2 {
			g.Skip("Skipped: All Worker nodes does not have at least two required free block disks/devices with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLvmCluster := newLvmCluster(setLvmClusterName(originLVMClusterName), setLvmClusterNamespace(lvmsNamespace))

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("#. Create a new LVMCluster resource with multiple device classes")
		lvmCluster := newLvmCluster(
			setLvmClusterName("test-lvmcluster-60835"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterDeviceClassName("vg1"),
			setLvmClusterDeviceClassName2("vg2"),
			setLvmClusterPaths(devicePaths),
		)
		err = lvmCluster.createWithMultiDeviceClasses()
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			lvmCluster.deleteSafely()

			g.By("#. Restoring original LVMCluster")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				originLvmCluster.createWithExportJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		err = lvmCluster.waitReady(10 * time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Check two lvms preset storage-classes are created one for each device class")
		checkStorageclassExists(storageClass1)
		checkStorageclassExists(storageClass2)

		sc1, err := tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass1, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("StorageClass %s fstype: %s\n", storageClass1, sc1.Parameters["csi.storage.k8s.io/fstype"])
		sc2, err := tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass2, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		logf("StorageClass %s fstype: %s\n", storageClass2, sc2.Parameters["csi.storage.k8s.io/fstype"])

		g.By("#. Create a new project/namespace for the scenario")
		testNs := "test-60835-" + fmt.Sprintf("%d", time.Now().Unix())
		err = createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Create a pvc-1 with the pre-set lvms csi storageclass-1")
		pvc1Name := "test-pvc1-60835"
		err = createPVCWithOC(pvcConfig{
			name:             pvc1Name,
			namespace:        testNs,
			storageClassName: storageClass1,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvc1Name, testNs)

		g.By("#. Create pod-1 with the created pvc-1 and wait for the pod-1 ready")
		mountPath := "/mnt/storage"
		pod1Name := "test-pod1-60835"
		err = createPodWithOC(podConfig{
			name:      pod1Name,
			namespace: testNs,
			pvcName:   pvc1Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", pod1Name, testNs)

		g.By("#. Wait for PVC-1 to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvc1Name, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod-1 to be ready")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), pod1Name, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check fileSystem type for pod-1 volume")
		nodeName := getNodeNameByPod(testNs, pod1Name)
		volName := getPVCVolumeName(testNs, pvc1Name)
		checkVolumeMountCmdContain(tc, volName, nodeName, "xfs")

		g.By("#. Write file to pod-1 volume")
		writePodData(tc, testNs, pod1Name, "test-container", mountPath)

		g.By("#. Create a pvc-2 with the pre-set lvms csi storageclass-2")
		pvc2Name := "test-pvc2-60835"
		err = createPVCWithOC(pvcConfig{
			name:             pvc2Name,
			namespace:        testNs,
			storageClassName: storageClass2,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvc2Name, testNs)

		g.By("#. Create pod-2 with the created pvc-2 and wait for the pod-2 ready")
		pod2Name := "test-pod2-60835"
		err = createPodWithOC(podConfig{
			name:      pod2Name,
			namespace: testNs,
			pvcName:   pvc2Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", pod2Name, testNs)

		g.By("#. Wait for PVC-2 to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvc2Name, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod-2 to be ready")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), pod2Name, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check fileSystem type for pod-2 volume")
		nodeName = getNodeNameByPod(testNs, pod2Name)
		volName = getPVCVolumeName(testNs, pvc2Name)
		checkVolumeMountCmdContain(tc, volName, nodeName, "ext4")

		g.By("#. Write file to pod-2 volume")
		writePodData(tc, testNs, pod2Name, "test-container", mountPath)

		g.By("#. Delete Pod and PVC")
		err = tc.Clientset.CoreV1().Pods(testNs).Delete(context.TODO(), pod1Name, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvc1Name, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().Pods(testNs).Delete(context.TODO(), pod2Name, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvc2Name, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete newly created LVMCluster resource")
		lvmCluster.deleteSafely()

		g.By("#. Check storageClasses are deleted successfully")
		checkResourcesNotExist("sc", storageClass1, "")
		checkResourcesNotExist("sc", storageClass2, "")

		g.By("#. Create original LVMCluster resource")
		err = originLvmCluster.createWithExportJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:rdeore-Critical-61586-[OTP][LVMS] [Block] Clone a pvc with Block VolumeMode", g.Label("SNO", "MNO"), func() {

		var (
			storageClassName = "lvms-" + volumeGroup
			devicePath       = "/dev/dblock"
			pvcOriName       = "test-pvc-block-ori"
		)

		g.By("#. Create new namespace for the test scenario")
		testNs := "test-61586-" + fmt.Sprintf("%d", time.Now().Unix())
		err := createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Create a PVC with Block volumeMode using lvms storageclass")
		err = createPVCWithOC(pvcConfig{
			name:             pvcOriName,
			namespace:        testNs,
			storageClassName: storageClassName,
			volumeMode:       "Block",
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcOriName, testNs)

		g.By("#. Create pod with the created pvc and wait for the pod ready")
		podOriName := "test-pod-block-ori"
		err = createPodWithOC(podConfig{
			name:      podOriName,
			namespace: testNs,
			pvcName:   pvcOriName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podOriName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Write data to raw block volume")
		writeDataIntoRawBlockVolume(tc, testNs, podOriName, "test-container", devicePath)
		execCommandInPod(tc, testNs, podOriName, "test-container", "sync")

		g.By("#. Create a clone pvc with Block volumeMode")
		pvcCloneName := "test-pvc-block-clone"
		err = createPVCWithOC(pvcConfig{
			name:             pvcCloneName,
			namespace:        testNs,
			storageClassName: storageClassName,
			volumeMode:       "Block",
			storage:          "1Gi",
			dataSourceName:   pvcOriName,
			dataSourceKind:   "PersistentVolumeClaim",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcCloneName, testNs)

		g.By("#. Create pod with the cloned pvc and wait for the pod ready")
		podCloneName := "test-pod-block-clone"
		err = createPodWithOC(podConfig{
			name:      podCloneName,
			namespace: testNs,
			pvcName:   pvcCloneName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podCloneName, testNs)

		g.By("#. Wait for cloned PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcCloneName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for clone pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podCloneName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Delete original pvc - should not impact the cloned one")
		err = tc.Clientset.CoreV1().Pods(testNs).Delete(context.TODO(), podOriName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcOriName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Check the data exists in cloned volume")
		checkDataInRawBlockVolume(tc, testNs, podCloneName, "test-container", devicePath)
	})

	g.It("Author:rdeore-Critical-61814-[OTP][LVMS] [Filesystem] [Clone] a pvc larger than disk size should be successful", g.Label("SNO", "MNO"), func() {

		var (
			storageClassName = "lvms-" + volumeGroup
			thinPoolName     = "thin-pool-1"
			mountPath        = "/mnt/storage"
			pvcOriName       = "test-pvc-clone-ori"
		)

		g.By("#. Create new namespace for the test scenario")
		testNs := "test-61814-" + fmt.Sprintf("%d", time.Now().Unix())
		err := createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Get thin pool size and calculate capacity bigger than disk size")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, thinPoolName)
		pvcCapacity := strconv.FormatInt(int64(thinPoolSize)+getRandomNum(2, 10), 10) + "Gi"
		logf("Using PVC capacity %s (thin pool size: %d Gi)\n", pvcCapacity, thinPoolSize)

		g.By("#. Create a PVC with capacity bigger than disk size")
		err = createPVCWithOC(pvcConfig{
			name:             pvcOriName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          pvcCapacity,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcOriName, testNs)

		g.By("#. Create pod with the created pvc and wait for the pod ready")
		podOriName := "test-pod-clone-ori"
		err = createPodWithOC(podConfig{
			name:      podOriName,
			namespace: testNs,
			pvcName:   pvcOriName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podOriName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcOriName, testNs, thinPoolSize)

		g.By("#. Write data to volume")
		writePodData(tc, testNs, podOriName, "test-container", mountPath)
		execCommandInPod(tc, testNs, podOriName, "test-container", "sync")

		g.By("#. Create a clone pvc with the same capacity")
		pvcCloneName := "test-pvc-clone-cloned"
		err = createPVCWithOC(pvcConfig{
			name:             pvcCloneName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          pvcCapacity,
			dataSourceName:   pvcOriName,
			dataSourceKind:   "PersistentVolumeClaim",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcCloneName, testNs)

		g.By("#. Create pod with the cloned pvc and wait for the pod ready")
		podCloneName := "test-pod-clone-cloned"
		err = createPodWithOC(podConfig{
			name:      podCloneName,
			namespace: testNs,
			pvcName:   pvcCloneName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podCloneName, testNs)

		g.By("#. Wait for cloned PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcCloneName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for clone pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podCloneName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check clone volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcCloneName, testNs, thinPoolSize)

		g.By("#. Delete original pvc - should not impact the cloned one")
		err = tc.Clientset.CoreV1().Pods(testNs).Delete(context.TODO(), podOriName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcOriName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Check the data exists in cloned volume")
		checkPodDataExists(tc, testNs, podCloneName, "test-container", mountPath, true)
	})

	g.It("Author:rdeore-Critical-61997-[OTP][LVMS] [Filesystem] [Snapshot] should restore volume larger than disk size with snapshot dataSource successfully and the volume could be read and written [Serial]", g.Label("SNO", "MNO"), func() {

		var (
			storageClassName        = "lvms-" + volumeGroup
			volumeSnapshotClassName = "lvms-" + volumeGroup
			thinPoolName            = "thin-pool-1"
			mountPath               = "/mnt/storage"
			pvcOriName              = "test-pvc-snap-ori"
			pvcRestoreName          = "test-pvc-snap-restore"
		)

		g.By("#. Create new namespace for the test scenario")
		testNs := "test-61997-" + fmt.Sprintf("%d", time.Now().Unix())
		err := createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Get thin pool size and calculate capacity bigger than disk size")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, thinPoolName)
		pvcCapacity := strconv.FormatInt(int64(thinPoolSize)+getRandomNum(2, 10), 10) + "Gi"
		logf("Using PVC capacity %s (thin pool size: %d Gi)\n", pvcCapacity, thinPoolSize)

		g.By("#. Create a PVC with capacity bigger than disk size")
		err = createPVCWithOC(pvcConfig{
			name:             pvcOriName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          pvcCapacity,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcOriName, testNs)

		g.By("#. Create pod with the created pvc and wait for the pod ready")
		podOriName := "test-pod-snap-ori"
		err = createPodWithOC(podConfig{
			name:      podOriName,
			namespace: testNs,
			pvcName:   pvcOriName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podOriName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcOriName, testNs, thinPoolSize)

		g.By("#. Write data to volume")
		writePodData(tc, testNs, podOriName, "test-container", mountPath)
		execCommandInPod(tc, testNs, podOriName, "test-container", "sync")

		g.By("#. Create volumesnapshot")
		snapshotName := "test-snapshot-61997"
		snapshotYAML := fmt.Sprintf(`apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: %s
  namespace: %s
spec:
  volumeSnapshotClassName: %s
  source:
    persistentVolumeClaimName: %s
`, snapshotName, testNs, volumeSnapshotClassName, pvcOriName)

		cmd := exec.Command("oc", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(snapshotYAML)
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to create snapshot: %s", string(output)))
		defer func() {
			exec.Command("oc", "delete", "volumesnapshot", snapshotName, "-n", testNs, "--ignore-not-found").Run()
		}()

		g.By("#. Wait for volumesnapshot to be ready")
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "volumesnapshot", snapshotName, "-n", testNs, "-o=jsonpath={.status.readyToUse}")
			output, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(output))
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal("true"))

		g.By("#. Create a restored pvc with snapshot dataSource")
		err = createPVCWithOC(pvcConfig{
			name:             pvcRestoreName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          pvcCapacity,
			dataSourceName:   snapshotName,
			dataSourceKind:   "VolumeSnapshot",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcRestoreName, testNs)

		g.By("#. Create pod with the restored pvc and wait for the pod ready")
		podRestoreName := "test-pod-snap-restore"
		err = createPodWithOC(podConfig{
			name:      podRestoreName,
			namespace: testNs,
			pvcName:   pvcRestoreName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podRestoreName, testNs)

		g.By("#. Wait for restored PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for restore pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check restored volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcRestoreName, testNs, thinPoolSize)

		g.By("#. Check the data exists in restored volume")
		checkPodDataExists(tc, testNs, podRestoreName, "test-container", mountPath, true)

		g.By("#. Check original pod and restored pod are deployed on same worker node")
		podOriObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		podRestoreObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podRestoreName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(podOriObj.Spec.NodeName).To(o.Equal(podRestoreObj.Spec.NodeName))
	})

	g.It("Author:rdeore-LEVEL0-Critical-61828-[OTP][LVMS] [Block] [Clone] a pvc larger than disk size should be successful", g.Label("SNO", "MNO"), func() {

		var (
			storageClassName = "lvms-" + volumeGroup
			thinPoolName     = "thin-pool-1"
			devicePath       = "/dev/dblock"
			pvcOriName       = "test-pvc-block-clone-ori"
		)

		g.By("#. Create new namespace for the test scenario")
		testNs := "test-61828-" + fmt.Sprintf("%d", time.Now().Unix())
		err := createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Get thin pool size and calculate capacity bigger than disk size")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, thinPoolName)
		pvcCapacity := strconv.FormatInt(int64(thinPoolSize)+getRandomNum(2, 10), 10) + "Gi"
		logf("Using PVC capacity %s (thin pool size: %d Gi)\n", pvcCapacity, thinPoolSize)

		g.By("#. Create a PVC with Block volumeMode and capacity bigger than disk size")
		err = createPVCWithOC(pvcConfig{
			name:             pvcOriName,
			namespace:        testNs,
			storageClassName: storageClassName,
			volumeMode:       "Block",
			storage:          pvcCapacity,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcOriName, testNs)

		g.By("#. Create pod with the created pvc and wait for the pod ready")
		podOriName := "test-pod-block-clone-ori"
		err = createPodWithOC(podConfig{
			name:      podOriName,
			namespace: testNs,
			pvcName:   pvcOriName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podOriName, testNs)

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podOriName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcOriName, testNs, thinPoolSize)

		g.By("#. Write data to raw block volume")
		writeDataIntoRawBlockVolume(tc, testNs, podOriName, "test-container", devicePath)
		execCommandInPod(tc, testNs, podOriName, "test-container", "sync")

		g.By("#. Create a clone pvc with Block volumeMode")
		pvcCloneName := "test-pvc-block-clone-cloned"
		err = createPVCWithOC(pvcConfig{
			name:             pvcCloneName,
			namespace:        testNs,
			storageClassName: storageClassName,
			volumeMode:       "Block",
			storage:          pvcCapacity,
			dataSourceName:   pvcOriName,
			dataSourceKind:   "PersistentVolumeClaim",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcCloneName, testNs)

		g.By("#. Create pod with the cloned pvc and wait for the pod ready")
		podCloneName := "test-pod-block-clone-cloned"
		err = createPodWithOC(podConfig{
			name:      podCloneName,
			namespace: testNs,
			pvcName:   pvcCloneName,
			mountPath: devicePath,
			isBlock:   true,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pod", podCloneName, testNs)

		g.By("#. Wait for cloned PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcCloneName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for clone pod to be running")
		o.Eventually(func() corev1.PodPhase {
			podObj, err := tc.Clientset.CoreV1().Pods(testNs).Get(context.TODO(), podCloneName, metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return podObj.Status.Phase
		}, PodReadyTimeout, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check clone volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, pvcCloneName, testNs, thinPoolSize)

		g.By("#. Delete original pvc - should not impact the cloned one")
		err = tc.Clientset.CoreV1().Pods(testNs).Delete(context.TODO(), podOriName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcOriName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Check the data exists in cloned volume")
		checkDataInRawBlockVolume(tc, testNs, podCloneName, "test-container", devicePath)
	})

	g.It("Author:mmakwana-High-83247-[OTP][LVMS] Verify that the LVMS PV label matches the node name [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		volumeGroup := "vg1"
		storageClassName := "lvms-" + volumeGroup
		mountPath := "/mnt/test"

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var diskName string
		isDiskFound := false
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				diskName = disk
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}
		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		cmd := exec.Command("oc", "get", "lvmcluster", "-n", lvmsNamespace, "-o=jsonpath={.items[0].metadata.name}")
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMClusterName := strings.TrimSpace(string(output))

		cmd = exec.Command("oc", "get", "lvmcluster", originLVMClusterName, "-n", lvmsNamespace, "-o", "json")
		outputJSON, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON := string(outputJSON)
		logf("Original LVMCluster JSON saved\n")

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)

		g.By("#. Create a new LVMCluster resource with specific paths")
		newLVMClusterName := "test-lvmcluster-83247"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName

		defer func() {
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)

			deadline := time.Now().Add(2 * time.Minute)
			for time.Now().Before(deadline) {
				exists, _ := resourceExists("lvmcluster", newLVMClusterName, lvmsNamespace)
				if !exists {
					break
				}
				time.Sleep(5 * time.Second)
			}

			cmd := exec.Command("oc", "get", "lvmcluster", originLVMClusterName, "-n", lvmsNamespace)
			if err := cmd.Run(); err != nil {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterForceWipeReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create new namespace for the test scenario")
		testNs := fmt.Sprintf("lvms-test-83247-%d", time.Now().UnixNano())
		err = createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Create a PVC")
		pvcName := "test-pvc-83247"
		err = createPVCWithOC(pvcConfig{
			name:             pvcName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "2Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvcName, testNs)

		g.By("#. Create a deployment")
		depName := "test-dep-83247"
		err = createDeploymentWithOC(deploymentConfig{
			name:      depName,
			namespace: testNs,
			pvcName:   pvcName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), depName, metav1.DeleteOptions{})

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), depName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcName, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, PVCBoundTimeout, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Write data in deployment pod")
		podList, err := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app=%s", depName),
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(podList.Items)).To(o.BeNumerically(">", 0))
		depPodName := podList.Items[0].Name
		writePodData(tc, testNs, depPodName, "test-container", mountPath)

		g.By("#. Check that PV's nodeAffinity hostnames match PVC's selected node")
		pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		pvName := pvcObj.Spec.VolumeName
		o.Expect(pvName).NotTo(o.BeEmpty())

		selectedNode := pvcObj.Annotations["volume.kubernetes.io/selected-node"]
		o.Expect(selectedNode).NotTo(o.BeEmpty(), "PVC should have selected-node annotation")
		logf("PVC selected node: %s\n", selectedNode)

		pv, err := tc.Clientset.CoreV1().PersistentVolumes().Get(context.TODO(), pvName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(pv.Spec.NodeAffinity).NotTo(o.BeNil())
		o.Expect(pv.Spec.NodeAffinity.Required).NotTo(o.BeNil())

		nodeAffinityMatched := false
		var nodeAffinityValues []string
		for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				for _, val := range expr.Values {
					nodeAffinityValues = append(nodeAffinityValues, val)
					if val == selectedNode {
						nodeAffinityMatched = true
					}
				}
			}
		}
		o.Expect(nodeAffinityMatched).To(o.BeTrue(), fmt.Sprintf("PV nodeAffinity should contain selected node %s", selectedNode))
		logf("PVC selected node %s must be allowed by PV nodeAffinity values: %v\n", selectedNode, nodeAffinityValues)

		g.By("#. Delete Deployment and PVC resources")
		err = tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), depName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcName, metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Restore original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:mmakwana-High-76425-[OTP][LVMS] Make thin pool overprovisionRatio editable in LVMS [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		volumeGroup := "vg1"
		storageClassName := "lvms-" + volumeGroup
		thinPoolName := "thin-pool-1"
		mountPath := "/mnt/test"

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(freeDiskNameCountMap) < 1 {
			g.Skip("Skipped: Cluster's Worker nodes does not have minimum required free block devices/disks attached")
		}

		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		var diskName string
		isDiskFound := false
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				diskName = disk
				isDiskFound = true
				delete(freeDiskNameCountMap, diskName)
				break
			}
		}
		if !isDiskFound {
			g.Skip("Skipped: All Worker nodes does not have a free block device/disk with same name attached")
		}

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		cmd := exec.Command("oc", "get", "lvmcluster", "-n", lvmsNamespace, "-o=jsonpath={.items[0].metadata.name}")
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMClusterName := strings.TrimSpace(string(output))

		cmd = exec.Command("oc", "get", "lvmcluster", originLVMClusterName, "-n", lvmsNamespace, "-o", "json")
		outputJSON, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON := string(outputJSON)
		logf("Original LVMCluster JSON saved\n")

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)
		defer func() {
			cmd := exec.Command("oc", "get", "lvmcluster", originLVMClusterName, "-n", lvmsNamespace)
			if err := cmd.Run(); err != nil {
				createLVMClusterFromJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("#. Create a new LVMCluster resource with specific paths")
		newLVMClusterName := "test-lvmcluster-76425"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName
		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create new namespace for the test scenario")
		testNs := fmt.Sprintf("lvms-test-76425-%d", time.Now().UnixNano())
		err = createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Get thin pool size and define PVC capacity bigger than disk size")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, thinPoolName)
		pvcCapacity := strconv.FormatInt(int64(thinPoolSize)+getRandomNum(10, 20), 10) + "Gi"
		logf("Using PVC capacity %s (thin pool size: %d Gi)\n", pvcCapacity, thinPoolSize)

		g.By("#. Create a pvc and deployment on each worker node with capacity bigger than disk size")
		var pvcNames []string
		var depNames []string

		for i, workerName := range workerNodes {
			pvcName := fmt.Sprintf("test-pvc%d-76425", i+1)
			depName := fmt.Sprintf("test-dep%d-76425", i+1)

			// Create PVC
			err = createPVCWithOC(pvcConfig{
				name:             pvcName,
				namespace:        testNs,
				storageClassName: storageClassName,
				storage:          pvcCapacity,
			})
			o.Expect(err).NotTo(o.HaveOccurred())
			pvcNames = append(pvcNames, pvcName)

			// Create Deployment with NodeSelector to pin to specific worker
			err = createDeploymentWithOC(deploymentConfig{
				name:         depName,
				namespace:    testNs,
				pvcName:      pvcName,
				mountPath:    mountPath,
				nodeSelector: workerName,
			})
			o.Expect(err).NotTo(o.HaveOccurred())
			depNames = append(depNames, depName)

			// Wait for deployment to be ready
			o.Eventually(func() bool {
				depObj, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), depName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return depObj.Status.ReadyReplicas == 1
			}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

			// Write data in deployment pod
			podList, err := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", depName),
			})
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(len(podList.Items)).To(o.BeNumerically(">", 0))
			writePodData(tc, testNs, podList.Items[0].Name, "test-container", mountPath)

			logf("Created PVC %s and Deployment %s on node %s\n", pvcName, depName, workerName)
		}

		g.By("#. Decrease overprovision ratio value to 1")
		err = patchOverprovisionRatio(newLVMClusterName, lvmsNamespace, "1")
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Check vg-manager pod status is Running and LVMCluster state is Ready")
		lvmClusterObj := &lvmCluster{
			name:            newLVMClusterName,
			namespace:       lvmsNamespace,
			deviceClassName: deviceClassName,
		}
		checkLVMClusterAndVGManagerPodReady(tc, lvmClusterObj)

		g.By("#. Create pvc2")
		pvc2Name := "test-pvc-final-76425"
		err = createPVCWithOC(pvcConfig{
			name:             pvc2Name,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer deleteSpecifiedResource("pvc", pvc2Name, testNs)

		g.By("#. Create deployment2")
		dep2Name := "test-dep-final-76425"
		err = createDeploymentWithOC(deploymentConfig{
			name:      dep2Name,
			namespace: testNs,
			pvcName:   pvc2Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), dep2Name, metav1.DeleteOptions{})

		g.By("#. Verify the PV provisioning failed due to not enough storage capacity")
		expectedErr := "NotEnoughCapacity"
		o.Eventually(func() string {
			cmd := exec.Command("oc", "describe", "pvc", pvc2Name, "-n", testNs)
			output, _ := cmd.CombinedOutput()
			return string(output)
		}, 120*time.Second, 5*time.Second).Should(o.ContainSubstring(expectedErr))

		g.By("#. Verify PVC stays in Pending state")
		o.Consistently(func() corev1.PersistentVolumeClaimPhase {
			pvcObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvc2Name, metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvcObj.Status.Phase
		}, 30*time.Second, 5*time.Second).Should(o.Equal(corev1.ClaimPending))
		logf("PVC %s is in Pending state as expected due to NotEnoughCapacity\n", pvc2Name)

		g.By("#. Delete Deployment and PVC resources")
		tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), dep2Name, metav1.DeleteOptions{})
		tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvc2Name, metav1.DeleteOptions{})
		for _, depName := range depNames {
			tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), depName, metav1.DeleteOptions{})
		}
		for _, pvcName := range pvcNames {
			tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcName, metav1.DeleteOptions{})
		}
		time.Sleep(10 * time.Second)

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Restore original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:mmakwana-High-86452-[LVMS] Verify LVMS allows removal of device classes on day 2 [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		volumeGroup1 := "vg1"
		volumeGroup2 := "vg2"
		storageClassName := "lvms-" + volumeGroup1
		storageClassName2 := "lvms-" + volumeGroup2

		g.By("#. Get list of worker nodes")
		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		var validDisks []string
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				validDisks = append(validDisks, disk)
			}
		}
		if len(validDisks) < 2 {
			g.Skip("Skipped: Cluster does not have 2 free disks present on all worker nodes")
		}
		diskName := validDisks[0]
		diskName2 := validDisks[1]
		if diskName > diskName2 {
			diskName, diskName2 = diskName2, diskName
		}
		logf("Selected disks dynamically: diskName=%s, diskName2=%s\n", diskName, diskName2)

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		originLvmCluster := newLvmCluster(setLvmClusterName(originLVMClusterName), setLvmClusterNamespace(lvmsNamespace))

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)
		defer func() {
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				logf("Restoring original LVMCluster %s...\n", originLVMClusterName)
				originLvmCluster.createWithExportJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("#. Wait for old CSIStorageCapacity objects to be cleaned up")
		o.Eventually(func() int {
			cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", lvmsNamespace,
				"-o=jsonpath={.items[?(@.storageClassName==\""+storageClassName+"\")].capacity}")
			output, _ := cmd.CombinedOutput()
			return len(strings.Fields(string(output)))
		}, 180*time.Second, 10*time.Second).Should(o.Equal(0))

		g.By("#. Create a new LVMCluster resource with two device classes")
		lvmCluster := newLvmCluster(
			setLvmClusterName("test-lvmcluster-86452"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterDeviceClassName(volumeGroup1),
			setLvmClusterDeviceClassName2(volumeGroup2),
			setLvmClusterFsType("xfs"),
			setLvmClusterPaths([]string{"/dev/" + diskName, "/dev/" + diskName2}),
		)
		err = lvmCluster.createWithTwoDeviceClasses()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			logf("Cleanup: Deleting test LVMCluster %s...\n", lvmCluster.name)
			deleteLVMClusterSafely(lvmCluster.name, lvmsNamespace, volumeGroup1)
		}()
		g.By("#. Wait for LVMCluster to be Ready")
		err = lvmCluster.waitReady(LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		checkStorageclassExists(storageClassName)
		checkStorageclassExists(storageClassName2)
		logf("Verified both storage classes %s and %s are present\n", storageClassName, storageClassName2)

		g.By("#. Create new namespace for the test scenario")
		testNs := fmt.Sprintf("test-86452-%d", time.Now().Unix())
		err = createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Create PVC and Deployment on vg2 to verify it is functional")
		pvcVg2Name := "test-pvc-vg2-86452"
		err = createPVCWithOC(pvcConfig{
			name:             pvcVg2Name,
			namespace:        testNs,
			storageClassName: storageClassName2,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		mountPath := "/mnt/storage"
		depVg2Name := "test-dep-vg2-86452"
		err = createDeploymentWithOC(deploymentConfig{
			name:      depVg2Name,
			namespace: testNs,
			pvcName:   pvcVg2Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for deployment to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), depVg2Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data to verify vg2 is functional")
		pods, err := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-vg2-86452",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podName := pods.Items[0].Name
		writePodData(tc, testNs, podName, "test-container", mountPath)
		logf("Verified vg2 device class is functional\n")

		g.By("#. Delete vg2 PVC and deployment to free storage before removal")
		tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), depVg2Name, metav1.DeleteOptions{})
		o.Eventually(func() bool {
			_, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), depVg2Name, metav1.GetOptions{})
			return err != nil
		}, ResourceDeleteTimeout, 5*time.Second).Should(o.BeTrue())

		tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcVg2Name, metav1.DeleteOptions{})
		o.Eventually(func() bool {
			_, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcVg2Name, metav1.GetOptions{})
			return err != nil
		}, ResourceDeleteTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Remove second device class from LVMCluster")
		err = lvmCluster.deleteSecondDeviceClass()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = lvmCluster.waitReady(LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Verify LVMCluster has only one device class")
		cmd := exec.Command("oc", "get", "lvmcluster", lvmCluster.name, "-n", lvmsNamespace,
			"-o=jsonpath={.spec.storage.deviceClasses[*].name}")
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(strings.Fields(string(output)))).To(o.Equal(1))

		g.By("#. Verify storageclass for vg2 is deleted")
		o.Eventually(func() bool {
			exists, _ := resourceExists("storageclass", storageClassName2, "")
			return !exists
		}, 2*time.Minute, 10*time.Second).Should(o.BeTrue())
		logf("Verified storageclass %s is deleted\n", storageClassName2)

		g.By("#. Verify vg2 volume group is removed from all worker nodes")
		for _, workerNode := range workerNodes {
			o.Eventually(func() bool {
				out := execCommandInNode(tc, workerNode, "vgs --noheadings -o vg_name | grep -w "+volumeGroup2+" || true")
				return strings.TrimSpace(out) == ""
			}, 5*time.Minute, 10*time.Second).Should(o.BeTrue())
		}
		logf("Verified %s volume group is removed from all worker nodes\n", volumeGroup2)

		g.By("#. Create PVC with vg1 storage class to verify it still works")
		pvcName := "test-pvc-vg1-86452"
		err = createPVCWithOC(pvcConfig{
			name:             pvcName,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		depName := "test-dep-vg1-86452"
		err = createDeploymentWithOC(deploymentConfig{
			name:      depName,
			namespace: testNs,
			pvcName:   pvcName,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for deployment to be ready")
		o.Eventually(func() bool {
			depObj, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), depName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return depObj.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data to vg1 volume")
		pods, err = tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-vg1-86452",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podNameVg1 := pods.Items[0].Name
		writePodData(tc, testNs, podNameVg1, "test-container", mountPath)
		logf("Successfully verified vg1 device class is still functional after removing vg2\n")

		g.By("#. Delete Deployment and PVC resources")
		tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), depName, metav1.DeleteOptions{GracePeriodSeconds: int64Ptr(0)})
		time.Sleep(5 * time.Second)
		pvName := ""
		pvcObj, _ := tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Get(context.TODO(), pvcName, metav1.GetOptions{})
		if pvcObj != nil {
			pvName = pvcObj.Spec.VolumeName
		}
		tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvcName, metav1.DeleteOptions{})
		if pvName != "" {
			cleanupLogicalVolumeByName(pvName)
			o.Eventually(func() bool {
				_, err := tc.Clientset.CoreV1().PersistentVolumes().Get(context.TODO(), pvName, metav1.GetOptions{})
				return err != nil
			}, 2*time.Minute, 5*time.Second).Should(o.BeTrue())
		}

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(lvmCluster.name, lvmsNamespace, volumeGroup1)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create original LVMCluster resource")
		err = originLvmCluster.createWithExportJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.It("Author:mmakwana-High-86156-[LVMS] Verify LVMS allows removal of devices on day 2 [Disruptive]", g.Label("SNO", "MNO", "Serial"), func() {

		volumeGroup1 := "vg1"
		storageClassName := "lvms-" + volumeGroup1

		g.By("#. Get list of worker nodes")
		workerNodes, err := getWorkersList()
		o.Expect(err).NotTo(o.HaveOccurred())
		workerNodeCount := len(workerNodes)

		g.By("#. Get list of available block devices/disks attached to all worker nodes")
		freeDiskNameCountMap, err := getListOfFreeDisksFromWorkerNodes(tc)
		o.Expect(err).NotTo(o.HaveOccurred())

		var validDisks []string
		for disk, count := range freeDiskNameCountMap {
			if count == int64(workerNodeCount) {
				validDisks = append(validDisks, disk)
			}
		}
		if len(validDisks) < 2 {
			g.Skip("Skipped: Cluster does not have 2 free disks present on all worker nodes")
		}
		diskName := validDisks[0]
		diskName2 := validDisks[1]
		if diskName > diskName2 {
			diskName, diskName2 = diskName2, diskName
		}
		logf("Selected disks dynamically: diskName=%s, diskName2=%s\n", diskName, diskName2)

		g.By("#. Copy and save existing LVMCluster configuration in JSON format")
		originLVMClusterName, err := getLVMClusterName(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())

		originLvmCluster := newLvmCluster(setLvmClusterName(originLVMClusterName), setLvmClusterNamespace(lvmsNamespace))

		g.By("#. Delete existing LVMCluster resource")
		deleteSpecifiedResource("lvmcluster", originLVMClusterName, lvmsNamespace)
		defer func() {
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				logf("Restoring original LVMCluster %s...\n", originLVMClusterName)
				originLvmCluster.createWithExportJSON(originLVMJSON)
			}
			waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		}()

		g.By("#. Wait for old CSIStorageCapacity objects to be cleaned up")
		o.Eventually(func() int {
			cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", lvmsNamespace,
				"-o=jsonpath={.items[?(@.storageClassName==\""+storageClassName+"\")].capacity}")
			output, _ := cmd.CombinedOutput()
			return len(strings.Fields(string(output)))
		}, 180*time.Second, 10*time.Second).Should(o.Equal(0))

		g.By("#. Create a new LVMCluster resource with two device paths")
		lvmCluster := newLvmCluster(
			setLvmClusterName("test-lvmcluster-86156"),
			setLvmClusterNamespace(lvmsNamespace),
			setLvmClusterDeviceClassName(volumeGroup1),
			setLvmClusterFsType("xfs"),
			setLvmClusterPaths([]string{"/dev/" + diskName, "/dev/" + diskName2}),
		)
		err = lvmCluster.createWithTwoPaths()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer func() {
			logf("Cleanup: Deleting test LVMCluster %s...\n", lvmCluster.name)
			deleteLVMClusterSafely(lvmCluster.name, lvmsNamespace, volumeGroup1)
		}()
		g.By("#. Wait for LVMCluster to be Ready")
		err = lvmCluster.waitReady(LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for both disks to be added to the volume group on all worker nodes")
		for _, workerNode := range workerNodes {
			o.Eventually(func() bool {
				output := execCommandInNode(tc, workerNode, "pvs --noheadings -o pv_name | grep -E '(/dev/"+diskName+"|/dev/"+diskName2+")' | wc -l")
				return strings.TrimSpace(output) == "2"
			}, 2*time.Minute, 5*time.Second).Should(o.BeTrue())
			logf("Both disks /dev/%s and /dev/%s are part of volume group on node: %s\n", diskName, diskName2, workerNode)
		}

		g.By("#. Create new namespace for the test scenario")
		testNs := fmt.Sprintf("test-86156-%d", time.Now().Unix())
		err = createNamespaceWithOC(testNs)
		o.Expect(err).NotTo(o.HaveOccurred())
		defer tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNs, metav1.DeleteOptions{})

		g.By("#. Create PVC and Deployment")
		mountPath := "/mnt/storage"
		pvc1Name := "test-pvc1-86156"
		err = createPVCWithOC(pvcConfig{
			name:             pvc1Name,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		dep1Name := "test-dep1-86156"
		err = createDeploymentWithOC(deploymentConfig{
			name:      dep1Name,
			namespace: testNs,
			pvcName:   pvc1Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for deployment to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), dep1Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment pod")
		pods, err := tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep1-86156",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podName1 := pods.Items[0].Name
		writePodData(tc, testNs, podName1, "test-container", mountPath)
		logf("Written data to volume successfully\n")

		g.By("#. Move extents from second disk to first disk on all worker nodes")
		for _, workerNode := range workerNodes {
			_, pvmoveErr := execCommandInNodeWithError(tc, workerNode, "pvmove /dev/"+diskName2+" /dev/"+diskName)
			o.Expect(pvmoveErr).NotTo(o.HaveOccurred())
			o.Eventually(func() bool {
				usedExtents, err := execCommandInNodeWithError(tc, workerNode, "pvs --noheadings -o pv_used /dev/"+diskName2)
				if err != nil {
					return false
				}
				logf("Node %s: %s extents left\n", workerNode, strings.TrimSpace(usedExtents))
				return strings.TrimSpace(usedExtents) == "0"
			}, 2*time.Minute, 5*time.Second).Should(o.BeTrue())

			logf("Data migration completed successfully on node: %s\n", workerNode)
		}

		g.By("#. Remove second disk from LVMCluster paths")
		err = lvmCluster.patchDevicePath([]string{"/dev/" + diskName})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = lvmCluster.waitReady(LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Verify second disk is removed from volume group on all worker nodes")
		for _, workerNode := range workerNodes {
			o.Eventually(func() bool {
				out := execCommandInNode(tc, workerNode, "pvs --noheadings | grep -w /dev/"+diskName2+" || true")
				return strings.TrimSpace(out) == ""
			}, 2*time.Minute, 5*time.Second).Should(o.BeTrue())
			logf("Disk /dev/%s successfully removed from volume group on node: %s\n", diskName2, workerNode)
		}

		g.By("#. Verify LVMCluster config is updated with only one disk path")
		cmd := exec.Command("oc", "get", "lvmcluster", lvmCluster.name, "-n", lvmsNamespace,
			"-o=jsonpath={.spec.storage.deviceClasses[0].deviceSelector.paths}")
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(string(output)).To(o.Equal("[\"/dev/" + diskName + "\"]"))
		logf("LVMCluster config contains only one disk: /dev/%s\n", diskName)

		g.By("#. Verify data written before pvmove still exists")
		checkPodDataExists(tc, testNs, podName1, "test-container", mountPath, true)
		logf("Data integrity verified after pvmove and device removal\n")

		g.By("#. Create another PVC and Deployment to verify VG still works")
		pvc2Name := "test-pvc2-86156"
		err = createPVCWithOC(pvcConfig{
			name:             pvc2Name,
			namespace:        testNs,
			storageClassName: storageClassName,
			storage:          "1Gi",
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		dep2Name := "test-dep2-86156"
		err = createDeploymentWithOC(deploymentConfig{
			name:      dep2Name,
			namespace: testNs,
			pvcName:   pvc2Name,
			mountPath: mountPath,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for deployment2 to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNs).Get(context.TODO(), dep2Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment2 pod")
		pods, err = tc.Clientset.CoreV1().Pods(testNs).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep2-86156",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podName2 := pods.Items[0].Name
		writePodData(tc, testNs, podName2, "test-container", mountPath)
		logf("Successfully verified VG still works after removing second disk\n")

		g.By("#. Delete Deployment and PVC resources")
		tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), dep2Name, metav1.DeleteOptions{})
		tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvc2Name, metav1.DeleteOptions{})
		tc.Clientset.AppsV1().Deployments(testNs).Delete(context.TODO(), dep1Name, metav1.DeleteOptions{})
		tc.Clientset.CoreV1().PersistentVolumeClaims(testNs).Delete(context.TODO(), pvc1Name, metav1.DeleteOptions{})
		time.Sleep(10 * time.Second)

		g.By("#. Delete newly created LVMCluster resource")
		deleteLVMClusterSafely(lvmCluster.name, lvmsNamespace, volumeGroup1)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create original LVMCluster resource")
		err = originLvmCluster.createWithExportJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, LVMClusterReadyTimeout)
		o.Expect(err).NotTo(o.HaveOccurred())
	})
})

func checkLvmsOperatorInstalled(tc *TestClient) {
	g.By("Checking if LVMS operator is installed")

	csiDrivers, err := tc.Clientset.StorageV1().CSIDrivers().List(context.TODO(), metav1.ListOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	csiDriverFound := false
	for _, driver := range csiDrivers.Items {
		if driver.Name == "topolvm.io" {
			csiDriverFound = true
			break
		}
	}

	if !csiDriverFound {
		g.Skip("LVMS Operator is not installed on the running OCP cluster")
	}

	_, err = tc.Clientset.CoreV1().Namespaces().Get(context.TODO(), lvmsNamespace, metav1.GetOptions{})
	if err != nil {
		g.Skip(fmt.Sprintf("LVMS namespace %s not found", lvmsNamespace))
	}

	logf("LVMS operator is installed and ready\n")
}
