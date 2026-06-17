package qe_tests

import (
	"context"
	"fmt"
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

		errMsg := "at least 1 valid device is required if DeviceSelector paths or optionalPaths are specified"
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
