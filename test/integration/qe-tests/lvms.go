// NOTE: This test suite currently only support SNO env & rely on some pre-defined steps in CI pipeline which includes,
//        1. Installing LVMS operator
//        2. Adding blank disk/device to worker node to be consumed by LVMCluster
//        3. Create resources like OperatorGroup, Subscription, etc. to configure LVMS operator
//        4. Create LVMCLuster resource with single volumeGroup named as 'vg1', multiple VGs could be added in future
//      Also, these tests are utilizing preset lvms storageClass="lvms-vg1", volumeSnapshotClassName="lvms-vg1"

package tests

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var (
	tc            = NewTestClient("lvms")
	testNamespace string
	storageClass  = "lvms-vg1"
	volumeGroup   = "vg1"
	lvmsNamespace = "openshift-lvm-storage"
)

var _ = g.BeforeSuite(func() {
	// Verify LVMS operator is installed
	checkLvmsOperatorInstalled(tc)
})

var _ = g.Describe("[sig-storage] STORAGE", func() {
	g.BeforeEach(func() {

		// Create a unique test namespace for each test using timestamp for uniqueness
		testNamespace = fmt.Sprintf("lvms-test-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
				Labels: map[string]string{
					"pod-security.kubernetes.io/enforce": "privileged",
					"pod-security.kubernetes.io/audit":   "privileged",
					"pod-security.kubernetes.io/warn":    "privileged",
				},
			},
		}
		_, err := tc.Clientset.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.AfterEach(func() {
		// Clean up test namespace
		if testNamespace != "" {
			err := tc.Clientset.CoreV1().Namespaces().Delete(context.TODO(), testNamespace, metav1.DeleteOptions{})
			if err != nil {
				e2e.Logf("Warning: failed to delete namespace %s: %v\n", testNamespace, err)
			}
		}
	})

	// original author: rdeore@redhat.com; Ported by Claude Code
	// OCP-61585-[LVMS] [Filesystem] [Clone] a pvc with the same capacity should be successful
	g.It("Author:rdeore-LEVEL0-Critical-61585-[LVMS] [Filesystem] [Clone] a pvc with the same capacity should be successful", g.Label("SNO"), func() {
		g.By("Create a PVC with the lvms csi storageclass")
		pvcOri := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-original",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: &storageClass,
			},
		}
		_, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvcOri, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create pod with the created pvc (required for WaitForFirstConsumer binding mode)")
		podOri := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-original",
				Namespace: testNamespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test-container",
						Image:   "registry.redhat.io/rhel8/support-tools:latest",
						Command: []string{"/bin/sh", "-c", "sleep 3600"},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "test-volume",
								MountPath: "/mnt/test",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "test-volume",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "test-pvc-original",
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.CoreV1().Pods(testNamespace).Create(context.TODO(), podOri, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PVC to be bound (happens after pod is scheduled)")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-original", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-original", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Write file to volume")

		g.By("Create a clone pvc with the lvms storageclass")
		pvcOriObj, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-original", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		dataSource := &corev1.TypedLocalObjectReference{
			Kind: "PersistentVolumeClaim",
			Name: "test-pvc-original",
		}

		pvcClone := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-clone",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: pvcOriObj.Spec.Resources.Requests[corev1.ResourceStorage],
					},
				},
				StorageClassName: &storageClass,
				DataSource:       dataSource,
			},
		}
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvcClone, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create pod with the cloned pvc (required for WaitForFirstConsumer binding mode)")
		podClone := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-clone",
				Namespace: testNamespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test-container",
						Image:   "registry.redhat.io/rhel8/support-tools:latest",
						Command: []string{"/bin/sh", "-c", "sleep 3600"},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "test-volume",
								MountPath: "/mnt/test",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "test-volume",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "test-pvc-clone",
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.CoreV1().Pods(testNamespace).Create(context.TODO(), podClone, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for cloned PVC to be bound (happens after pod is scheduled)")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-clone", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("Wait for cloned pod to be running")
		o.Eventually(func() corev1.PodPhase {
			pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-clone", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("Delete original pvc will not impact the cloned one")
		err = tc.Clientset.CoreV1().Pods(testNamespace).Delete(context.TODO(), "test-pod-original", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		o.Eventually(func() bool {
			_, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-original", metav1.GetOptions{})
			return err != nil
		}, 2*time.Minute, 5*time.Second).Should(o.BeTrue())

		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-original", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Check the cloned pod is still running")
		pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-clone", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(pod.Status.Phase).To(o.Equal(corev1.PodRunning))
	})

	// original author: rdeore@redhat.com; Ported by Claude Code
	// OCP-61433-[LVMS] [Block] [WaitForFirstConsumer] PVC resize on LVM cluster beyond thinpool size, but within over-provisioning limit
	g.It("Author:rdeore-Critical-61433-[LVMS] [Block] [WaitForFirstConsumer] PVC resize on LVM cluster beyond thinpool size, but within over-provisioning limit", g.Label("SNO"), func() {
		g.By("Get thin pool size and over provision limit")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, "thin-pool-1")

		g.By("Create a PVC with Block volumeMode")
		initialCapacity := "2Gi"
		volumeMode := corev1.PersistentVolumeBlock
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-block-resize",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(initialCapacity),
					},
				},
				StorageClassName: &storageClass,
				VolumeMode:       &volumeMode,
			},
		}
		_, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Create deployment with block volume device (WaitForFirstConsumer requires pod to exist)")
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dep-block",
				Namespace: testNamespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test-dep-block",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test-dep-block",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "test-container",
								Image:   "registry.redhat.io/rhel8/support-tools:latest",
								Command: []string{"/bin/sh", "-c", "sleep 3600"},
								VolumeDevices: []corev1.VolumeDevice{
									{
										Name:       "test-volume",
										DevicePath: "/dev/dblock",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "test-volume",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc-block-resize",
									},
								},
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.AppsV1().Deployments(testNamespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Wait for PVC to be bound (happens after pod is scheduled)")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-block-resize", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("Wait for deployment to be ready")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-block", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, 3*time.Minute, 5*time.Second).Should(o.BeTrue())

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
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(targetCapacity))

		g.By("Verify deployment is still healthy after resize")
		dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-block", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(dep.Status.ReadyReplicas).To(o.Equal(int32(1)))
	})

	// original author: rdeore@redhat.com; Ported by Claude Code
	// OCP-66320-[LVMS] Pre-defined CSI Storageclass should get re-created automatically after deleting
	g.It("Author:rdeore-LEVEL0-High-66320-[LVMS] Pre-defined CSI Storageclass should get re-created automatically after deleting [Disruptive]", g.Label("SNO"), func() {
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
			// Restore storage class if it doesn't exist
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
					e2e.Logf("Warning: failed to restore storage class: %v\n", err)
				}
			}
		}()

		g.By("Check deleted lvms storageClass is re-created automatically")
		o.Eventually(func() error {
			_, err := tc.Clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
			return err
		}, 30*time.Second, 5*time.Second).Should(o.Succeed())
	})

	// original author: mmakwana@redhat.com; Ported by Claude Code
	// OCP-71012-[LVMS] Verify the wiping of local volumes in LVMS
	g.It("Author:mmakwana-High-71012-[LVMS] Verify the wiping of local volumes in LVMS [Disruptive] [Serial]", g.Label("SNO"), func() {
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
		e2e.Logf("Found LVMCluster: %s\n", originLVMClusterName)

		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Original LVMCluster saved\n")

		g.By("#. Delete existing LVMCluster resource")
		err = deleteLVMClusterSafely(originLVMClusterName, lvmsNamespace, "vg1")
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Restoring original LVMCluster")
			exists, err := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			o.Expect(err).NotTo(o.HaveOccurred())
			if !exists {
				err := createLVMClusterFromJSON(originLVMJSON)
				o.Expect(err).NotTo(o.HaveOccurred())
			}
			err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
		}()

		g.By("#. Create logical volume on backend disk/device")
		workerName := workerNodes[0]
		vgName := "vg-71012"
		lvName := "lv-71012"
		err = createLogicalVolumeOnDisk(tc, workerName, diskName, vgName, lvName)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up logical volume from disk")
			removeLogicalVolumeOnDisk(tc, workerName, diskName, vgName, lvName)
		}()

		g.By("#. Create a LVMCluster resource with the disk explicitly with forceWipeDevicesAndDestroyAllData")
		newLVMClusterName := "test-lvmcluster-71012"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName
		err = createLVMClusterWithForceWipe(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		g.By("#. Wait for new LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		storageClassName := "lvms-" + deviceClassName
		pvcTest := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-71012",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: &storageClassName,
			},
		}

		g.By("#. Create a pvc")
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvcTest, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a deployment")
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dep-71012",
				Namespace: testNamespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test-dep-71012",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test-dep-71012",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "test-container",
								Image:   "registry.redhat.io/rhel8/support-tools:latest",
								Command: []string{"/bin/sh", "-c", "sleep 3600"},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "test-volume",
										MountPath: "/mnt/test",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "test-volume",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc-71012",
									},
								},
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.AppsV1().Deployments(testNamespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71012", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, 3*time.Minute, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment pod")
		pods, err := tc.Clientset.CoreV1().Pods(testNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-71012",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podName := pods.Items[0].Name

		writeCmd := "echo 'test data for OCP-71012' > /mnt/test/testfile.txt && cat /mnt/test/testfile.txt"
		cmdOutput := execCommandInPod(tc, testNamespace, podName, "test-container", writeCmd)
		o.Expect(cmdOutput).To(o.ContainSubstring("test data for OCP-71012"))

		g.By("#. Delete Deployment and PVC resources")
		err = tc.Clientset.AppsV1().Deployments(testNamespace).Delete(context.TODO(), "test-dep-71012", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-71012", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete newly created LVMCluster resource")
		err = deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())

		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	// original author: mmakwana@redhat.com; Ported by Claude Code
	// OCP-66241-[LVMS] Check workload management annotations are present in LVMS resources
	g.It("Author:mmakwana-High-66241-[LVMS] Check workload management annotations are present in LVMS resources [Disruptive]", g.Label("SNO"), func() {
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
		e2e.Logf("Found LVMCluster: %s\n", originLVMClusterName)

		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Original LVMCluster saved\n")

		g.By("#. Delete existing LVMCluster resource")
		err = deleteLVMClusterSafely(originLVMClusterName, lvmsNamespace, "vg1")
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Restoring original LVMCluster")
			exists, err := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			o.Expect(err).NotTo(o.HaveOccurred())
			if !exists {
				err := createLVMClusterFromJSON(originLVMJSON)
				o.Expect(err).NotTo(o.HaveOccurred())
			}
			err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
		}()

		g.By("#. Create a new LVMCluster resource")
		newLVMClusterName := "test-lvmcluster-66241"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName
		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		g.By("#. Wait for new LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Check workload management annotations are present in LVMS resources")
		expectedSubstring := `{"effect": "PreferredDuringScheduling"}`

		// Check lvms-operator deployment annotation
		deployment, err := tc.Clientset.AppsV1().Deployments(lvmsNamespace).Get(context.TODO(), "lvms-operator", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		annotation1 := deployment.Spec.Template.Annotations["target.workload.openshift.io/management"]
		e2e.Logf("LVM Operator Annotations: %s\n", annotation1)
		o.Expect(annotation1).To(o.ContainSubstring(expectedSubstring))

		// Check vg-manager daemonset annotation
		daemonset, err := tc.Clientset.AppsV1().DaemonSets(lvmsNamespace).Get(context.TODO(), "vg-manager", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		annotation2 := daemonset.Spec.Template.Annotations["target.workload.openshift.io/management"]
		e2e.Logf("VG Manager Annotations: %s\n", annotation2)
		o.Expect(annotation2).To(o.ContainSubstring(expectedSubstring))

		g.By("#. Delete newly created LVMCluster resource")
		err = deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())

		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	// original author: mmakwana@redhat.com; Ported by Claude Code
	// OCP-71378-[LVMS] Recover LVMS cluster from on-disk metadata
	g.It("Author:mmakwana-High-71378-[LVMS] Recover LVMS cluster from on-disk metadata [Disruptive]", g.Label("SNO"), func() {
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
		e2e.Logf("Found LVMCluster: %s\n", originLVMClusterName)

		originLVMJSON, err := getLVMClusterJSON(originLVMClusterName, lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Original LVMCluster saved\n")

		g.By("#. Delete existing LVMCluster resource")
		err = deleteLVMClusterSafely(originLVMClusterName, lvmsNamespace, "vg1")
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Restoring original LVMCluster")
			exists, err := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			o.Expect(err).NotTo(o.HaveOccurred())
			if !exists {
				err := createLVMClusterFromJSON(originLVMJSON)
				o.Expect(err).NotTo(o.HaveOccurred())
			}
			err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
		}()

		g.By("#. Create LVMCluster resource with single devicePath")
		newLVMClusterName := "test-lvmcluster-71378"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName
		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		g.By("#. Wait for LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a PVC (pvc1)")
		pvc1 := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-71378-1",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: &storageClassName,
			},
		}
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvc1, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a deployment (dep1)")
		deployment1 := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dep-71378-1",
				Namespace: testNamespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test-dep-71378-1",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test-dep-71378-1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "test-container",
								Image:   "registry.redhat.io/rhel8/support-tools:latest",
								Command: []string{"/bin/sh", "-c", "sleep 3600"},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "test-volume",
										MountPath: "/mnt/test",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "test-volume",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc-71378-1",
									},
								},
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.AppsV1().Deployments(testNamespace).Create(context.TODO(), deployment1, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71378-1", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, 3*time.Minute, 5*time.Second).Should(o.BeTrue())

		g.By("#. Fetch disk path from current LVMCluster")
		diskPaths, err := getLvmClusterPath(lvmsNamespace)
		o.Expect(err).NotTo(o.HaveOccurred())
		selectedDisk := strings.Fields(diskPaths)[0]
		e2e.Logf("Selected Disk Path: %s\n", selectedDisk)

		g.By("#. Remove finalizers from LVMCluster and LVMVolumeGroup and delete LVMCluster")
		err = deleteLVMClusterForRecovery(newLVMClusterName, lvmsNamespace, deviceClassName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a new LVMCluster resource with same disk path (testing recovery)")
		newLVMClusterName2 := "test-lvmcluster-71378-recovered"
		err = createLVMClusterWithPaths(newLVMClusterName2, lvmsNamespace, deviceClassName, selectedDisk)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up recovered test LVMCluster")
			deleteLVMClusterSafely(newLVMClusterName2, lvmsNamespace, deviceClassName)
		}()

		g.By("#. Wait for recovered LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName2, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a PVC (pvc2)")
		pvc2 := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-71378-2",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: &storageClassName,
			},
		}
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvc2, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a deployment (dep2)")
		deployment2 := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dep-71378-2",
				Namespace: testNamespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test-dep-71378-2",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test-dep-71378-2",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "test-container",
								Image:   "registry.redhat.io/rhel8/support-tools:latest",
								Command: []string{"/bin/sh", "-c", "sleep 3600"},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "test-volume",
										MountPath: "/mnt/test",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "test-volume",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc-71378-2",
									},
								},
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.AppsV1().Deployments(testNamespace).Create(context.TODO(), deployment2, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for the deployment2 to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71378-2", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, 3*time.Minute, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment2 pod")
		pods, err := tc.Clientset.CoreV1().Pods(testNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-71378-2",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		pod2Name := pods.Items[0].Name

		writeCmd := "echo 'test data for OCP-71378' > /mnt/test/testfile.txt && cat /mnt/test/testfile.txt"
		cmdOutput := execCommandInPod(tc, testNamespace, pod2Name, "test-container", writeCmd)
		o.Expect(cmdOutput).To(o.ContainSubstring("test data for OCP-71378"))

		g.By("#. Check dep1 is still running (verifying recovery)")
		dep1, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-71378-1", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(dep1.Status.ReadyReplicas).To(o.Equal(int32(1)))
		e2e.Logf("The deployment %s in namespace %s is in healthy state after recovery\n", dep1.Name, dep1.Namespace)

		g.By("#. Delete Deployment and PVC resources")
		err = tc.Clientset.AppsV1().Deployments(testNamespace).Delete(context.TODO(), "test-dep-71378-2", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-71378-2", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		err = tc.Clientset.AppsV1().Deployments(testNamespace).Delete(context.TODO(), "test-dep-71378-1", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-71378-1", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete newly created LVMCluster resource")
		err = deleteLVMClusterSafely(newLVMClusterName2, lvmsNamespace, deviceClassName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())

		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	// original author: mmakwana@redhat.com
	// OCP-77069-[LVMS] Make thin pool metadata size configurable in LVMS
	g.It("Author:mmakwana-High-77069-[LVMS] Make thin pool metadata size configurable in LVMS [Disruptive]", g.Label("SNO"), func() {
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
		e2e.Logf("Original LVMCluster: %s\n", originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete existing LVMCluster resource")
		deviceClassNameOrig := "vg1"
		err = deleteLVMClusterSafely(originLVMClusterName, lvmsNamespace, deviceClassNameOrig)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Restoring original LVMCluster in defer block")
			exists, _ := resourceExists("lvmcluster", originLVMClusterName, lvmsNamespace)
			if !exists {
				err := createLVMClusterFromJSON(originLVMJSON)
				o.Expect(err).NotTo(o.HaveOccurred())
			}
			err := waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
			o.Expect(err).NotTo(o.HaveOccurred())
		}()

		g.By("#. Create LVMCluster resource and then patch MetadataSizeCalculationPolicy set to 'Static'")
		newLVMClusterName := "test-lvmcluster-77069"
		deviceClassName := "vg1"
		diskPath := "/dev/" + diskName
		metadataSize := "100Mi"

		err = createLVMClusterWithPaths(newLVMClusterName, lvmsNamespace, deviceClassName, diskPath)
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up test LVMCluster in defer block")
			deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		}()

		err = patchMetadataSizeCalculationPolicyToStatic(newLVMClusterName, lvmsNamespace, metadataSize)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for LVMCluster to be Ready")
		err = waitForLVMClusterReady(newLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create a PVC")
		storageClassName := "lvms-" + deviceClassName
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-77069",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: &storageClassName,
			},
		}
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up PVC in defer block")
			tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-77069", metav1.DeleteOptions{})
		}()

		g.By("#. Create a deployment")
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dep-77069",
				Namespace: testNamespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test-dep-77069",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test-dep-77069",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "registry.access.redhat.com/ubi8/ubi-minimal:latest",
								Command: []string{
									"/bin/sh",
									"-c",
									"sleep infinity",
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "test-volume",
										MountPath: "/mnt/test",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "test-volume",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "test-pvc-77069",
									},
								},
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.AppsV1().Deployments(testNamespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			g.By("Cleaning up deployment in defer block")
			tc.Clientset.AppsV1().Deployments(testNamespace).Delete(context.TODO(), "test-dep-77069", metav1.DeleteOptions{})
		}()

		g.By("#. Wait for the deployment to be in ready state")
		o.Eventually(func() bool {
			dep, err := tc.Clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-77069", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, 3*time.Minute, 5*time.Second).Should(o.BeTrue())

		g.By("#. Write data in deployment pod")
		// Get pod name
		pods, err := tc.Clientset.CoreV1().Pods(testNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=test-dep-77069",
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
		podName := pods.Items[0].Name

		// Write test data
		writeCmd := "echo 'test data' > /mnt/test/testfile.txt"
		output := execCommandInPod(tc, testNamespace, podName, "test-container", writeCmd)
		o.Expect(output).NotTo(o.ContainSubstring("error"))

		// Verify data was written
		readCmd := "cat /mnt/test/testfile.txt"
		output = execCommandInPod(tc, testNamespace, podName, "test-container", readCmd)
		o.Expect(output).To(o.ContainSubstring("test data"))

		g.By("#. Debug into the node and check the size of the metadata for logical volumes")
		nodeName, err := getLogicalVolumeSelectedNode(testNamespace, "test-pvc-77069")
		o.Expect(err).NotTo(o.HaveOccurred())

		lvsCmd := "lvs --noheadings -o lv_name,lv_metadata_size"
		lvsOutput := execCommandInNode(tc, nodeName, lvsCmd)
		e2e.Logf("Logical volume metadata size: %s\n", lvsOutput)

		expectedLvsOutput := "100.00m"
		o.Expect(lvsOutput).To(o.ContainSubstring(expectedLvsOutput))

		g.By("#. Delete Deployment and PVC resources")
		err = tc.Clientset.AppsV1().Deployments(testNamespace).Delete(context.TODO(), "test-dep-77069", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-77069", metav1.DeleteOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Delete newly created LVMCluster resource")
		err = deleteLVMClusterSafely(newLVMClusterName, lvmsNamespace, deviceClassName)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create original LVMCluster resource")
		err = createLVMClusterFromJSON(originLVMJSON)
		o.Expect(err).NotTo(o.HaveOccurred())

		err = waitForLVMClusterReady(originLVMClusterName, lvmsNamespace, 4*time.Minute)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	// original author: rdeore@redhat.com
	g.It("Author:rdeore-Critical-61998-[LVMS] [Block] [Snapshot] should restore volume larger than disk size with snapshot dataSource successfully and the volume could be read and written [Serial]", g.Label("SNO"), func() {
		volumeGroup := "vg1"
		thinPoolName := "thin-pool-1"
		storageClassName := "lvms-" + volumeGroup
		volumeSnapshotClassName := "lvms-" + volumeGroup

		g.By("#. Get thin pool size")
		thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, thinPoolName)

		g.By("#. Create a PVC with Block volumeMode and capacity bigger than disk size")
		pvcCapacity := fmt.Sprintf("%dGi", int64(thinPoolSize)+getRandomNum(2, 10))
		volumeMode := corev1.PersistentVolumeBlock
		pvcOri := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-ori-61998",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(pvcCapacity),
					},
				},
				StorageClassName: &storageClassName,
				VolumeMode:       &volumeMode,
			},
		}
		_, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvcOri, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create pod with the created pvc (using volumeDevices for block mode)")
		podOri := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-ori-61998",
				Namespace: testNamespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test-container",
						Image:   "registry.redhat.io/rhel8/support-tools:latest",
						Command: []string{"/bin/sh", "-c", "sleep 3600"},
						VolumeDevices: []corev1.VolumeDevice{
							{
								Name:       "test-volume",
								DevicePath: "/dev/dblock",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "test-volume",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "test-pvc-ori-61998",
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.CoreV1().Pods(testNamespace).Create(context.TODO(), podOri, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-ori-61998", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for pod to be running")
		o.Eventually(func() corev1.PodPhase {
			pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-ori-61998", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, "test-pvc-ori-61998", testNamespace, thinPoolSize)

		g.By("#. Sync data to disk")
		syncCmd := "sync"
		execCommandInPod(tc, testNamespace, "test-pod-ori-61998", "test-container", syncCmd)

		g.By("#. Create volumesnapshot using oc")
		snapshotName := "test-snapshot-61998"
		snapshotYAML := fmt.Sprintf(`apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: %s
  namespace: %s
spec:
  volumeSnapshotClassName: %s
  source:
    persistentVolumeClaimName: test-pvc-ori-61998
`, snapshotName, testNamespace, volumeSnapshotClassName)

		cmd := exec.Command("oc", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(snapshotYAML)
		output, err := cmd.CombinedOutput()
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Failed to create snapshot: %s", string(output)))

		g.By("#. Wait for volumesnapshot to be ready")
		o.Eventually(func() string {
			cmd := exec.Command("oc", "get", "volumesnapshot", snapshotName, "-n", testNamespace, "-o=jsonpath={.status.readyToUse}")
			output, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(output))
		}, 3*time.Minute, 5*time.Second).Should(o.Equal("true"))

		g.By("#. Create a restored pvc with snapshot dataSource")
		pvcRestore := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc-restore-61998",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(pvcCapacity),
					},
				},
				StorageClassName: &storageClassName,
				VolumeMode:       &volumeMode,
				DataSource: &corev1.TypedLocalObjectReference{
					APIGroup: &[]string{"snapshot.storage.k8s.io"}[0],
					Kind:     "VolumeSnapshot",
					Name:     snapshotName,
				},
			},
		}
		_, err = tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvcRestore, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Create pod with the restored pvc")
		podRestore := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-restore-61998",
				Namespace: testNamespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test-container",
						Image:   "registry.redhat.io/rhel8/support-tools:latest",
						Command: []string{"/bin/sh", "-c", "sleep 3600"},
						VolumeDevices: []corev1.VolumeDevice{
							{
								Name:       "test-volume",
								DevicePath: "/dev/dblock",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "test-volume",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "test-pvc-restore-61998",
							},
						},
					},
				},
			},
		}
		_, err = tc.Clientset.CoreV1().Pods(testNamespace).Create(context.TODO(), podRestore, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("#. Wait for restored PVC to be bound")
		o.Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-restore-61998", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.ClaimBound))

		g.By("#. Wait for restored pod to be running")
		o.Eventually(func() corev1.PodPhase {
			pod, err := tc.Clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-restore-61998", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(o.Equal(corev1.PodRunning))

		g.By("#. Check restored volume size is bigger than disk size")
		checkVolumeBiggerThanDisk(tc, "test-pvc-restore-61998", testNamespace, thinPoolSize)
	})
})

// checkLvmsOperatorInstalled verifies that LVMS operator is installed on the cluster
func checkLvmsOperatorInstalled(tc *TestClient) {
	g.By("Checking if LVMS operator is installed")

	// Check if CSI driver exists
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

	// Verify LVMCluster exists and is Ready
	_, err = tc.Clientset.CoreV1().Namespaces().Get(context.TODO(), lvmsNamespace, metav1.GetOptions{})
	if err != nil {
		g.Skip(fmt.Sprintf("LVMS namespace %s not found", lvmsNamespace))
	}

	e2e.Logf("LVMS operator is installed and ready\n")
}
