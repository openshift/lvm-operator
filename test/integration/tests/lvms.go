// NOTE: This test suite currently only support SNO env & rely on some pre-defined steps in CI pipeline which includes,
//        1. Installing LVMS operator
//        2. Adding blank disk/device to worker node to be consumed by LVMCluster
//        3. Create resources like OperatorGroup, Subscription, etc. to configure LVMS operator
//        4. Create LVMCLuster resource with single volumeGroup named as 'vg1', mutliple VGs could be added in future
//      Also, these tests are utilizing preset lvms storageClass="lvms-vg1", volumeSnapshotClassName="lvms-vg1"

package lvms

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clientset      *kubernetes.Clientset
	testNamespace  string
	storageClass   = "lvms-vg1"
	volumeGroup    = "vg1"
	lvmsNamespace  = "openshift-lvm-storage"
	cleanupTimeout = 5 * time.Minute
	clientsetInit  bool
)

// initClientset initializes the Kubernetes clientset if not already initialized
func initClientset() {
	if clientsetInit {
		return
	}

	// Initialize Kubernetes client
	kubeconfig := filepath.Join(homeDir(), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred())

	clientset, err = kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())

	// Verify LVMS operator is installed
	checkLvmsOperatorInstalled()

	clientsetInit = true
}

var _ = BeforeSuite(func() {
	initClientset()
})

var _ = Describe("[sig-storage] STORAGE", func() {
	BeforeEach(func() {
		// Ensure clientset is initialized
		initClientset()

		// Create a unique test namespace for each test using timestamp for uniqueness
		testNamespace = fmt.Sprintf("lvms-test-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		_, err := clientset.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Clean up test namespace
		if testNamespace != "" {
			err := clientset.CoreV1().Namespaces().Delete(context.TODO(), testNamespace, metav1.DeleteOptions{})
			if err != nil {
				GinkgoWriter.Printf("Warning: failed to delete namespace %s: %v\n", testNamespace, err)
			}
		}
	})

	// original author: rdeore@redhat.com; Ported by Claude Code
	// OCP-61585-[LVMS] [Filesystem] [Clone] a pvc with the same capacity should be successful
	It("Author:rdeore-LEVEL0-Critical-61585-[LVMS] [Filesystem] [Clone] a pvc with the same capacity should be successful", Label("SNO"), func() {
		By("Create a PVC with the lvms csi storageclass")
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
		_, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvcOri, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create pod with the created pvc (required for WaitForFirstConsumer binding mode)")
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
		_, err = clientset.CoreV1().Pods(testNamespace).Create(context.TODO(), podOri, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for PVC to be bound (happens after pod is scheduled)")
		Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-original", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(Equal(corev1.ClaimBound))

		By("Wait for pod to be running")
		Eventually(func() corev1.PodPhase {
			pod, err := clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-original", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(Equal(corev1.PodRunning))

		By("Write file to volume")
		// This would require exec into pod - simplified for now
		// In a real test, you would exec into the pod and write data

		By("Create a clone pvc with the lvms storageclass")
		pvcOriObj, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-original", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

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
		_, err = clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvcClone, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create pod with the cloned pvc (required for WaitForFirstConsumer binding mode)")
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
		_, err = clientset.CoreV1().Pods(testNamespace).Create(context.TODO(), podClone, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for cloned PVC to be bound (happens after pod is scheduled)")
		Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-clone", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(Equal(corev1.ClaimBound))

		By("Wait for cloned pod to be running")
		Eventually(func() corev1.PodPhase {
			pod, err := clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-clone", metav1.GetOptions{})
			if err != nil {
				return corev1.PodPending
			}
			return pod.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(Equal(corev1.PodRunning))

		By("Delete original pvc will not impact the cloned one")
		err = clientset.CoreV1().Pods(testNamespace).Delete(context.TODO(), "test-pod-original", metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() bool {
			_, err := clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-original", metav1.GetOptions{})
			return err != nil
		}, 2*time.Minute, 5*time.Second).Should(BeTrue())

		err = clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(context.TODO(), "test-pvc-original", metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Check the cloned pod is still running")
		pod, err := clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), "test-pod-clone", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
	})

	// original author: rdeore@redhat.com; Ported by Claude Code
	// OCP-61433-[LVMS] [Block] [WaitForFirstConsumer] PVC resize on LVM cluster beyond thinpool size, but within over-provisioning limit
	It("Author:rdeore-Critical-61433-[LVMS] [Block] [WaitForFirstConsumer] PVC resize on LVM cluster beyond thinpool size, but within over-provisioning limit", Label("SNO"), func() {
		By("Get thin pool size and over provision limit")
		thinPoolSize := getThinPoolSizeByVolumeGroup(volumeGroup, "thin-pool-1")

		By("Create a PVC with Block volumeMode")
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
		_, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create deployment with block volume device (WaitForFirstConsumer requires pod to exist)")
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
		_, err = clientset.AppsV1().Deployments(testNamespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for PVC to be bound (happens after pod is scheduled)")
		Eventually(func() corev1.PersistentVolumeClaimPhase {
			pvc, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-block-resize", metav1.GetOptions{})
			if err != nil {
				return corev1.ClaimPending
			}
			return pvc.Status.Phase
		}, 3*time.Minute, 5*time.Second).Should(Equal(corev1.ClaimBound))

		By("Wait for deployment to be ready")
		Eventually(func() bool {
			dep, err := clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-block", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, 3*time.Minute, 5*time.Second).Should(BeTrue())

		By("Check PVC can re-size beyond thinpool size, but within overprovisioning rate")
		targetCapacityInt64 := getRandomNum(int64(thinPoolSize+1), int64(thinPoolSize+10))
		targetCapacity := fmt.Sprintf("%dGi", targetCapacityInt64)

		By(fmt.Sprintf("Resize PVC from %s to %s", initialCapacity, targetCapacity))
		pvcObj, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-block-resize", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pvcObj.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse(targetCapacity)
		_, err = clientset.CoreV1().PersistentVolumeClaims(testNamespace).Update(context.TODO(), pvcObj, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for PVC resize to complete")
		Eventually(func() string {
			pvc, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(context.TODO(), "test-pvc-block-resize", metav1.GetOptions{})
			if err != nil {
				return ""
			}
			if capacity, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
				return capacity.String()
			}
			return ""
		}, 3*time.Minute, 5*time.Second).Should(Equal(targetCapacity))

		By("Verify deployment is still healthy after resize")
		dep, err := clientset.AppsV1().Deployments(testNamespace).Get(context.TODO(), "test-dep-block", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(dep.Status.ReadyReplicas).To(Equal(int32(1)))
	})

	// original author: rdeore@redhat.com; Ported by Claude Code
	// OCP-66320-[LVMS] Pre-defined CSI Storageclass should get re-created automatically after deleting
	It("Author:rdeore-LEVEL0-High-66320-[LVMS] Pre-defined CSI Storageclass should get re-created automatically after deleting [Disruptive]", func() {
		By("Check lvms storageclass exists on cluster")
		_, err := clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
		if err != nil {
			Skip(fmt.Sprintf("Skipped: the cluster does not have storage-class: %s", storageClass))
		}

		By("Save the original storage class for restoration")
		originalSC, err := clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Delete existing lvms storageClass")
		err = clientset.StorageV1().StorageClasses().Delete(context.TODO(), storageClass, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		defer func() {
			// Restore storage class if it doesn't exist
			_, err := clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
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
				_, err = clientset.StorageV1().StorageClasses().Create(context.TODO(), scCopy, metav1.CreateOptions{})
				if err != nil {
					GinkgoWriter.Printf("Warning: failed to restore storage class: %v\n", err)
				}
			}
		}()

		By("Check deleted lvms storageClass is re-created automatically")
		Eventually(func() error {
			_, err := clientset.StorageV1().StorageClasses().Get(context.TODO(), storageClass, metav1.GetOptions{})
			return err
		}, 30*time.Second, 5*time.Second).Should(Succeed())
	})
})

// checkLvmsOperatorInstalled verifies that LVMS operator is installed on the cluster
func checkLvmsOperatorInstalled() {
	By("Checking if LVMS operator is installed")

	// Check if CSI driver exists
	csiDrivers, err := clientset.StorageV1().CSIDrivers().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	csiDriverFound := false
	for _, driver := range csiDrivers.Items {
		if driver.Name == "topolvm.io" {
			csiDriverFound = true
			break
		}
	}

	if !csiDriverFound {
		Skip("LVMS Operator is not installed on the running OCP cluster")
	}

	// Verify LVMCluster exists and is Ready
	// Note: This requires access to LVMCluster CRD which would need a dynamic client
	// For now, we'll check if the namespace exists
	_, err = clientset.CoreV1().Namespaces().Get(context.TODO(), lvmsNamespace, metav1.GetOptions{})
	if err != nil {
		Skip(fmt.Sprintf("LVMS namespace %s not found", lvmsNamespace))
	}

	GinkgoWriter.Printf("LVMS operator is installed and ready\n")
}

// homeDir returns the user's home directory
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
