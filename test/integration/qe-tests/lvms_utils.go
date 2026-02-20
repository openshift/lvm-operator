package tests

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	o "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

// int32Ptr returns a pointer to an int32
func int32Ptr(i int32) *int32 {
	return &i
}

// boolPtr returns a pointer to a bool
func boolPtr(b bool) *bool {
	return &b
}

// getRandomNum returns a random number between m and n (inclusive)
func getRandomNum(m int64, n int64) int64 {
	rand.Seed(time.Now().UnixNano())
	return rand.Int63n(n-m+1) + m
}

// getThinPoolSizeByVolumeGroup gets the total thin pool size for a given volume group from all worker nodes
func getThinPoolSizeByVolumeGroup(tc *TestClient, volumeGroup string, thinPoolName string) int {
	// Use lvs with specific VG/LV selection to avoid complex shell piping
	cmd := fmt.Sprintf("lvs --units g --noheadings -o lv_size %s/%s 2>/dev/null || echo 0", volumeGroup, thinPoolName)

	// Get list of worker nodes
	nodes, err := tc.Clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/worker",
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	var totalThinPoolSize int = 0

	for _, node := range nodes.Items {
		output := execCommandInNode(tc, node.Name, cmd)
		if output == "" {
			continue
		}

		regexForNumbersOnly := regexp.MustCompile("[0-9.]+")
		matches := regexForNumbersOnly.FindAllString(output, -1)
		if len(matches) == 0 {
			continue
		}

		sizeVal := matches[0]
		sizeNum := strings.Split(sizeVal, ".")
		if len(sizeNum) == 0 {
			continue
		}

		thinPoolSize, err := strconv.Atoi(sizeNum[0])
		if err != nil {
			continue
		}
		totalThinPoolSize = totalThinPoolSize + thinPoolSize
	}

	e2e.Logf("Total thin pool size in Gi from backend nodes: %d\n", totalThinPoolSize)
	return totalThinPoolSize
}

// execCommandInNode executes a command in a specific node using debug pod
func execCommandInNode(tc *TestClient, nodeName string, command string) string {
	// Create a debug pod on the specific node
	debugPodName := fmt.Sprintf("debug-%s-%d", nodeName, time.Now().Unix())
	debugPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      debugPodName,
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			HostNetwork:   true,
			HostPID:       true,
			HostIPC:       true,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "debug",
					Image:   "registry.redhat.io/rhel8/support-tools:latest",
					Command: []string{"/bin/sh", "-c", "sleep 3600"},
					SecurityContext: &corev1.SecurityContext{
						Privileged: boolPtr(true),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "host",
							MountPath: "/host",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "host",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/",
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
		},
	}

	_, err := tc.Clientset.CoreV1().Pods("default").Create(context.TODO(), debugPod, metav1.CreateOptions{})
	if err != nil {
		e2e.Logf("Failed to create debug pod: %v\n", err)
		return ""
	}

	defer func() {
		_ = tc.Clientset.CoreV1().Pods("default").Delete(context.TODO(), debugPodName, metav1.DeleteOptions{})
	}()

	// Wait for pod to be running
	o.Eventually(func() bool {
		pod, err := tc.Clientset.CoreV1().Pods("default").Get(context.TODO(), debugPodName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		return pod.Status.Phase == corev1.PodRunning
	}, 2*time.Minute, 5*time.Second).Should(o.BeTrue())

	// Execute command in the pod using nsenter to access host namespaces
	// nsenter properly handles stdin for commands like cryptsetup
	escapedCmd := strings.ReplaceAll(command, "'", "'\\''")
	wrappedCmd := fmt.Sprintf("nsenter --target 1 --mount --uts --ipc --net --pid -- /bin/bash -c '%s'", escapedCmd)
	output := execCommandInPod(tc, "default", debugPodName, "debug", wrappedCmd)

	return output
}

// execCommandInPod executes a command in a pod
func execCommandInPod(tc *TestClient, namespace, podName, containerName, command string) string {
	// Create a copy of config to avoid modifying the global config
	config := *tc.Config
	// Skip TLS verification to avoid certificate issues
	config.Insecure = true
	config.TLSClientConfig.Insecure = true
	config.TLSClientConfig.CAData = nil
	config.TLSClientConfig.CAFile = ""

	req := tc.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   []string{"/bin/sh", "-c", command},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(&config, "POST", req.URL())
	if err != nil {
		e2e.Logf("Failed to create executor: %v\n", err)
		return ""
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		e2e.Logf("Failed to execute command: %v, stderr: %s\n", err, stderr.String())
		return ""
	}

	return strings.TrimSpace(stdout.String())
}

// getWorkersList returns the list of worker node names
func getWorkersList() ([]string, error) {
	cmd := exec.Command("kubectl", "get", "nodes", "-l", "node-role.kubernetes.io/worker", "-o=jsonpath={.items[*].metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker nodes: %w, output: %s", err, string(output))
	}
	workers := strings.Fields(string(output))
	return workers, nil
}

// getListOfFreeDisksFromWorkerNodes gets the list of unused free block devices/disks along with their total count from all the worker nodes
func getListOfFreeDisksFromWorkerNodes(tc *TestClient) (map[string]int64, error) {
	// Check for mock mode (for CI environments without real disks)
	if mockDisk := os.Getenv("LVMS_MOCK_FREE_DISK"); mockDisk != "" {
		e2e.Logf("⚙ MOCK MODE: Using disk %s from LVMS_MOCK_FREE_DISK env variable\n", mockDisk)
		workerNodes, err := getWorkersList()
		if err != nil {
			return nil, err
		}
		e2e.Logf("  Simulating disk %s available on all %d worker node(s)\n", mockDisk, len(workerNodes))
		return map[string]int64{mockDisk: int64(len(workerNodes))}, nil
	}

	freeDiskNamesCount := make(map[string]int64)
	workerNodes, err := getWorkersList()
	if err != nil {
		return nil, err
	}

	e2e.Logf("\n========== DISK DISCOVERY STARTED ==========\n")
	e2e.Logf("Scanning for free disks on %d worker node(s)...\n", len(workerNodes))

	for _, workerName := range workerNodes {
		isDiskFound := false
		e2e.Logf("\n[Node: %s]\n", workerName)
		e2e.Logf("  Running: lsblk | grep disk | awk \"{print $1}\"\n")

		output := execCommandInNode(tc, workerName, `lsblk | grep disk | awk "{print \$1}"`)
		if output == "" {
			e2e.Logf("  ⚠ WARNING: No disks found or lsblk command failed (empty output)\n")
			continue
		}

		e2e.Logf("  Raw disk list:\n")
		diskList := strings.Split(output, "\n")
		for _, diskLine := range diskList {
			diskLine = strings.TrimSpace(diskLine)
			if diskLine != "" {
				e2e.Logf("    - %s\n", diskLine)
			}
		}

		e2e.Logf("  Checking disk availability with blkid:\n")
		for _, diskName := range diskList {
			diskName = strings.TrimSpace(diskName)
			if diskName == "" {
				continue
			}

			blkidCmd := "blkid /dev/" + diskName
			e2e.Logf("    Running: %s\n", blkidCmd)
			output := execCommandInNode(tc, workerName, blkidCmd)

			// disks that are used by existing LVMCluster have TYPE='LVM' OR Unused free disk does not return any output
			if strings.Contains(output, "LVM") || len(strings.TrimSpace(output)) == 0 {
				freeDiskNamesCount[diskName] = freeDiskNamesCount[diskName] + 1
				isDiskFound = true // at least 1 required free disk found
				if output == "" {
					e2e.Logf("      ✓ /dev/%s is FREE (no filesystem signature)\n", diskName)
				} else {
					e2e.Logf("      ✓ /dev/%s is available (LVM-managed): %s\n", diskName, output)
				}
			} else {
				e2e.Logf("      ✗ /dev/%s is IN USE: %s\n", diskName, output)
			}
		}

		if !isDiskFound {
			e2e.Logf("  ⚠ WARNING: Worker node %s does not have mandatory unused free block device/disk attached\n", workerName)
		}
	}

	e2e.Logf("\n========== DISK DISCOVERY SUMMARY ==========\n")
	if len(freeDiskNamesCount) == 0 {
		e2e.Logf("  ✗ NO FREE DISKS FOUND on any worker node\n")
	} else {
		e2e.Logf("  Free disks found across nodes:\n")
		for disk, count := range freeDiskNamesCount {
			e2e.Logf("    - /dev/%s: available on %d/%d nodes\n", disk, count, len(workerNodes))
		}
	}
	e2e.Logf("===========================================\n\n")

	return freeDiskNamesCount, nil
}

// createLogicalVolumeOnDisk makes a disk partition and creates a logical volume on new volume group
func createLogicalVolumeOnDisk(tc *TestClient, nodeHostName string, disk string, vgName string, lvName string) error {
	diskName := "/dev/" + disk

	// Create LVM disk partition
	createPartitionCmd := "echo -e 'n\\np\\n1\\n\\n\\nw' | fdisk " + diskName
	_, err := execCommandInNodeWithError(tc, nodeHostName, createPartitionCmd)
	if err != nil {
		return fmt.Errorf("failed to create partition: %w", err)
	}

	partitionName := diskName + "p1"
	// Unmount the partition if it's mounted
	unmountCmd := "umount " + partitionName + " || true"
	execCommandInNode(tc, nodeHostName, unmountCmd)

	// Create Physical Volume
	createPV := "pvcreate " + partitionName
	_, err = execCommandInNodeWithError(tc, nodeHostName, createPV)
	if err != nil {
		return fmt.Errorf("failed to create PV: %w", err)
	}

	// Create Volume Group
	createVG := "vgcreate " + vgName + " " + partitionName
	_, err = execCommandInNodeWithError(tc, nodeHostName, createVG)
	if err != nil {
		return fmt.Errorf("failed to create VG: %w", err)
	}

	// Create Logical Volume
	createLV := "lvcreate -n " + lvName + " -l 100%FREE " + vgName
	_, err = execCommandInNodeWithError(tc, nodeHostName, createLV)
	if err != nil {
		return fmt.Errorf("failed to create LV: %w", err)
	}

	return nil
}

// removeLogicalVolumeOnDisk removes logical volume on volume group from backend disk
func removeLogicalVolumeOnDisk(tc *TestClient, nodeHostName string, disk string, vgName string, lvName string) error {
	diskName := "/dev/" + disk
	partitionName := disk + "p1"
	pvName := diskName + "p1"

	existsLV := `lvdisplay /dev/` + vgName + `/` + lvName + ` && echo "true" || echo "false"`
	outputLV := execCommandInNode(tc, nodeHostName, existsLV)
	lvExists := strings.Contains(outputLV, "true")

	// If VG exists, proceed to check LV and remove accordingly
	existsVG := `vgdisplay | grep -q '` + vgName + `' && echo "true" || echo "false"`
	outputVG := execCommandInNode(tc, nodeHostName, existsVG)
	if strings.Contains(outputVG, "true") {
		if lvExists {
			// Remove Logical Volume (LV)
			removeLV := "lvremove -f /dev/" + vgName + "/" + lvName
			execCommandInNode(tc, nodeHostName, removeLV)
		}
		// Remove Volume Group (VG)
		removeVG := "vgremove -f " + vgName
		execCommandInNode(tc, nodeHostName, removeVG)
	}

	existsPV := `pvdisplay | grep -q '` + pvName + `' && echo "true" || echo "false"`
	outputPV := execCommandInNode(tc, nodeHostName, existsPV)
	if strings.Contains(outputPV, "true") {
		//Remove Physical Volume (PV)
		removePV := "pvremove -f " + pvName
		execCommandInNode(tc, nodeHostName, removePV)
	}

	existsPartition := `lsblk | grep -q '` + partitionName + `' && echo "true" || echo "false"`
	outputPartition := execCommandInNode(tc, nodeHostName, existsPartition)
	if strings.Contains(outputPartition, "true") {
		// Remove LVM disk partition
		removePartitionCmd := "echo -e 'd\\nw' | fdisk " + diskName
		execCommandInNode(tc, nodeHostName, removePartitionCmd)
	}

	// Wipe all filesystem signatures from disk
	wipeDiskCmd := "wipefs -a " + diskName
	execCommandInNode(tc, nodeHostName, wipeDiskCmd)

	return nil
}

// execCommandInNodeWithError executes a command in a node and returns output and error
func execCommandInNodeWithError(tc *TestClient, nodeName string, command string) (string, error) {
	output := execCommandInNode(tc, nodeName, command)
	if strings.Contains(strings.ToLower(output), "error") || strings.Contains(strings.ToLower(output), "failed") {
		return output, fmt.Errorf("command failed: %s", output)
	}
	return output, nil
}

// getLVMClusterJSON retrieves the LVMCluster resource as JSON
func getLVMClusterJSON(name string, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "get", "lvmcluster", name, "-n", namespace, "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get LVMCluster: %w, output: %s", err, string(output))
	}
	return string(output), nil
}

// deleteLVMCluster deletes an LVMCluster resource
func deleteLVMCluster(name string, namespace string) error {
	cmd := exec.Command("kubectl", "delete", "lvmcluster", name, "-n", namespace, "--ignore-not-found", "--wait=false")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete LVMCluster: %w, output: %s", err, string(output))
	}
	return nil
}

// createLVMClusterFromJSON creates an LVMCluster from JSON content
func createLVMClusterFromJSON(jsonContent string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(jsonContent)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create LVMCluster: %w, output: %s", err, string(output))
	}
	return nil
}

// createLVMClusterWithForceWipe creates an LVMCluster with forceWipeDevicesAndDestroyAllData set to true
func createLVMClusterWithForceWipe(name string, namespace string, deviceClass string, diskPath string) error {
	lvmClusterYAML := fmt.Sprintf(`apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: %s
  namespace: %s
spec:
  storage:
    deviceClasses:
    - name: %s
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10
      deviceSelector:
        paths:
        - %s
        forceWipeDevicesAndDestroyAllData: true
`, name, namespace, deviceClass, diskPath)

	return createLVMClusterFromJSON(lvmClusterYAML)
}

// createLVMClusterWithPaths creates an LVMCluster with specified device paths
func createLVMClusterWithPaths(name string, namespace string, deviceClass string, diskPath string) error {
	lvmClusterYAML := fmt.Sprintf(`apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: %s
  namespace: %s
spec:
  storage:
    deviceClasses:
    - name: %s
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10
      deviceSelector:
        paths:
        - %s
`, name, namespace, deviceClass, diskPath)

	return createLVMClusterFromJSON(lvmClusterYAML)
}

// waitForLVMClusterReady waits for the LVMCluster to become Ready
func waitForLVMClusterReady(name string, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("kubectl", "get", "lvmcluster", name, "-n", namespace, "-o=jsonpath={.status.state}")
		output, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) == "Ready" {
			e2e.Logf("LVMCluster %s is Ready\n", name)
			return nil
		}
		e2e.Logf("LVMCluster %s state: %s, waiting...\n", name, string(output))
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for LVMCluster %s to become Ready", name)
}

// removeLVMClusterFinalizers removes finalizers from LVMCluster to allow deletion
func removeLVMClusterFinalizers(name string, namespace string) error {
	patch := `{"metadata":{"finalizers":[]}}`
	cmd := exec.Command("kubectl", "patch", "lvmcluster", name, "-n", namespace, "--type=merge", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove LVMCluster finalizers: %w, output: %s", err, string(output))
	}
	return nil
}

// removeLVMVolumeGroupFinalizers removes finalizers from LVMVolumeGroup
func removeLVMVolumeGroupFinalizers(deviceClassName string, namespace string) error {
	// Check if resource exists first
	checkCmd := exec.Command("kubectl", "get", "lvmvolumegroup", deviceClassName, "-n", namespace, "--ignore-not-found", "-o=name")
	checkOutput, _ := checkCmd.CombinedOutput()
	if len(strings.TrimSpace(string(checkOutput))) == 0 {
		// Resource doesn't exist, skip patching
		return nil
	}

	patch := `{"metadata":{"finalizers":[]}}`
	cmd := exec.Command("kubectl", "patch", "lvmvolumegroup", deviceClassName, "-n", namespace, "--type=merge", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove LVMVolumeGroup finalizers: %w, output: %s", err, string(output))
	}
	return nil
}

// removeLVMVolumeGroupNodeStatusFinalizers removes finalizers from all LVMVolumeGroupNodeStatus resources
func removeLVMVolumeGroupNodeStatusFinalizers(namespace string) error {
	workerNodes, err := getWorkersList()
	if err != nil {
		return err
	}

	for _, workerName := range workerNodes {
		// Check if resource exists first
		checkCmd := exec.Command("kubectl", "get", "lvmvolumegroupnodestatus", workerName, "-n", namespace, "--ignore-not-found", "-o=name")
		checkOutput, _ := checkCmd.CombinedOutput()
		if len(strings.TrimSpace(string(checkOutput))) == 0 {
			// Resource doesn't exist, skip patching
			continue
		}

		patch := `{"metadata":{"finalizers":[]}}`
		cmd := exec.Command("kubectl", "patch", "lvmvolumegroupnodestatus", workerName, "-n", namespace, "--type=merge", "-p", patch)
		output, err := cmd.CombinedOutput()
		if err != nil {
			e2e.Logf("Warning: failed to remove finalizers from LVMVolumeGroupNodeStatus %s: %v, output: %s\n", workerName, err, string(output))
		}
	}
	return nil
}

// deleteLVMClusterSafely deletes an LVMCluster by removing finalizers after deletion
func deleteLVMClusterSafely(name string, namespace string, deviceClassName string) error {
	// Delete the LVMCluster first with --wait=false
	err := deleteLVMCluster(name, namespace)
	if err != nil {
		return err
	}

	// Then remove finalizers from all related resources
	removeLVMVolumeGroupNodeStatusFinalizers(namespace)
	removeLVMVolumeGroupFinalizers(deviceClassName, namespace)
	removeLVMClusterFinalizers(name, namespace)

	// Wait for LVMCluster to be fully deleted
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		exists, err := resourceExists("lvmcluster", name, namespace)
		if err != nil {
			return fmt.Errorf("failed to check if LVMCluster exists: %w", err)
		}
		if !exists {
			e2e.Logf("LVMCluster %s fully deleted\n", name)
			return nil
		}
		e2e.Logf("Waiting for LVMCluster %s to be deleted...\n", name)
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for LVMCluster %s to be deleted", name)
}

// deleteLVMClusterForRecovery deletes an LVMCluster but does NOT wait for backend cleanup
// This allows the backend VG to remain on disk for recovery testing
func deleteLVMClusterForRecovery(name string, namespace string, deviceClassName string) error {
	// Step 1: Initiate delete WITHOUT waiting (matches original test behavior)
	e2e.Logf("Initiating delete of LVMCluster %s (without waiting)...\n", name)
	cmd := exec.Command("kubectl", "delete", "lvmcluster", name, "-n", namespace, "--wait=false")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to initiate LVMCluster deletion: %w, output: %s", err, string(output))
	}

	// Step 2: Immediately remove finalizers (this prevents backend cleanup)
	e2e.Logf("Removing finalizers to prevent backend VG cleanup for %s...\n", name)
	time.Sleep(2 * time.Second) // Small delay to let deletion start

	removeLVMClusterFinalizers(name, namespace)
	removeLVMVolumeGroupFinalizers(deviceClassName, namespace)
	removeLVMVolumeGroupNodeStatusFinalizers(namespace)

	// Step 3: Wait for LVMCluster to be fully deleted from Kubernetes
	e2e.Logf("Waiting for LVMCluster %s to be deleted from Kubernetes...\n", name)
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		exists, err := resourceExists("lvmcluster", name, namespace)
		if err != nil {
			return fmt.Errorf("failed to check if LVMCluster exists: %w", err)
		}
		if !exists {
			e2e.Logf("LVMCluster %s deleted from Kubernetes (backend VG will remain for recovery)\n", name)
			return nil
		}
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for LVMCluster %s to be deleted", name)
}

// getLVMClusterName retrieves the first LVMCluster name from a given namespace
func getLVMClusterName(namespace string) (string, error) {
	cmd := exec.Command("kubectl", "get", "lvmcluster", "-n", namespace, "-o=jsonpath={.items[0].metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get LVMCluster name: %w, output: %s", err, string(output))
	}
	name := strings.TrimSpace(string(output))
	if name == "" {
		return "", fmt.Errorf("no LVMCluster found in namespace %s", namespace)
	}
	return name, nil
}

// resourceExists checks if a Kubernetes resource exists
func resourceExists(resourceType string, name string, namespace string) (bool, error) {
	cmd := exec.Command("kubectl", "get", resourceType, name, "-n", namespace, "--ignore-not-found", "-o=jsonpath={.metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to check resource existence: %w, output: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)) != "", nil
}

// getLvmClusterPath gets the current LVM cluster device path
func getLvmClusterPath(namespace string) (string, error) {
	currentLVMClusterName, err := getLVMClusterName(namespace)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("kubectl", "get", "lvmcluster", "-n", namespace, currentLVMClusterName, "-o=jsonpath={.status.deviceClassStatuses[*].nodeStatus[*].devices[*]}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get LVMCluster path: %w, output: %s", err, string(output))
	}
	selectedDisk := strings.TrimSpace(string(output))
	e2e.Logf("Current LVM cluster path: %s\n", selectedDisk)
	return selectedDisk, nil
}

// patchMetadataSizeCalculationPolicyToStatic patches the LVMCluster to set metadataSizeCalculationPolicy to Static with the given metadataSize
func patchMetadataSizeCalculationPolicyToStatic(name string, namespace string, metadataSize string) error {
	patch := fmt.Sprintf(`[
		{"op": "replace", "path": "/spec/storage/deviceClasses/0/thinPoolConfig/metadataSizeCalculationPolicy", "value": "Static"},
		{"op": "replace", "path": "/spec/storage/deviceClasses/0/thinPoolConfig/metadataSize", "value": "%s"}
	]`, metadataSize)

	cmd := exec.Command("kubectl", "patch", "lvmcluster", name, "-n", namespace, "--type=json", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to patch LVMCluster metadataSize: %w, output: %s", err, string(output))
	}
	e2e.Logf("Patched LVMCluster %s with metadataSizeCalculationPolicy set to 'Static' and metadataSize to %s\n", name, metadataSize)
	return nil
}

// getLogicalVolumeSelectedNode gets the node where the LVMS provisioned volume is located
func getLogicalVolumeSelectedNode(namespace string, pvcName string) (string, error) {
	cmd := exec.Command("kubectl", "get", "pvc", pvcName, "-n", namespace, "-o=jsonpath={.metadata.annotations.volume\\.kubernetes\\.io/selected-node}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get selected node for PVC %s: %w, output: %s", pvcName, err, string(output))
	}
	nodeName := strings.TrimSpace(string(output))
	e2e.Logf("The nodename in namespace %s for pvc %s is %s\n", namespace, pvcName, nodeName)
	return nodeName, nil
}

// patchOverprovisionRatio patches the LVMCluster to set overprovisionRatio
func patchOverprovisionRatio(name string, namespace string, overprovisionRatio string) error {
	patch := fmt.Sprintf(`[
		{"op": "replace", "path": "/spec/storage/deviceClasses/0/thinPoolConfig/overprovisionRatio", "value": %s}
	]`, overprovisionRatio)

	cmd := exec.Command("kubectl", "patch", "lvmcluster", name, "-n", namespace, "--type=json", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to patch LVMCluster overprovisionRatio: %w, output: %s", err, string(output))
	}
	e2e.Logf("Patched LVMCluster %s with overprovisionRatio=%s\n", name, overprovisionRatio)
	return nil
}

// waitForVGManagerPodRunning waits for vg-manager pods to be in Running state
func waitForVGManagerPodRunning(tc *TestClient, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pods, err := tc.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/component=vg-manager",
		})
		if err != nil {
			e2e.Logf("Failed to list vg-manager pods: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(pods.Items) == 0 {
			e2e.Logf("No vg-manager pods found, waiting...\n")
			time.Sleep(5 * time.Second)
			continue
		}

		allRunning := true
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				allRunning = false
				e2e.Logf("vg-manager pod %s is in %s phase, waiting...\n", pod.Name, pod.Status.Phase)
				break
			}
		}

		if allRunning {
			e2e.Logf("All vg-manager pods are Running\n")
			return nil
		}

		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for vg-manager pods to be Running")
}

// getVolSizeFromPvc gets the volume size from PVC status
func getVolSizeFromPvc(tc *TestClient, pvcName string, namespace string) (string, error) {
	pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PVC %s in namespace %s: %w", pvcName, namespace, err)
	}

	if pvc.Status.Capacity == nil {
		return "", fmt.Errorf("PVC %s has no capacity in status", pvcName)
	}

	volumeSize := pvc.Status.Capacity[corev1.ResourceStorage]
	volumeSizeStr := volumeSize.String()
	e2e.Logf("The PVC %s volumesize is %s\n", pvcName, volumeSizeStr)
	return volumeSizeStr, nil
}

// checkVolumeBiggerThanDisk verifies that the PV size is bigger than the thin pool size
func checkVolumeBiggerThanDisk(tc *TestClient, pvcName string, pvcNamespace string, thinPoolSize int) {
	pvSize, err := getVolSizeFromPvc(tc, pvcName, pvcNamespace)
	o.Expect(err).NotTo(o.HaveOccurred())

	// Extract numeric value from size string (e.g., "10Gi" -> "10")
	regexForNumbersOnly := regexp.MustCompile("[0-9]+")
	pvSizeVal := regexForNumbersOnly.FindAllString(pvSize, -1)[0]
	pvSizeNum, err := strconv.Atoi(pvSizeVal)
	o.Expect(err).NotTo(o.HaveOccurred())

	e2e.Logf("Persistent volume Size in Gi: %d\n", pvSizeNum)
	o.Expect(pvSizeNum > thinPoolSize).Should(o.BeTrue())
}

// writePodData writes test data to the mounted volume in a pod
func writePodData(tc *TestClient, namespace string, podName string, containerName string, mountPath string) {
	writeCmd := fmt.Sprintf("echo 'storage test' > %s/testfile", mountPath)
	output := execCommandInPod(tc, namespace, podName, containerName, writeCmd)
	e2e.Logf("Write command output: %s\n", output)

	syncCmd := fmt.Sprintf("sync -f %s/testfile", mountPath)
	output = execCommandInPod(tc, namespace, podName, containerName, syncCmd)
	e2e.Logf("Sync command output: %s\n", output)
}

// checkPodDataExists verifies that test data exists in the mounted volume
func checkPodDataExists(tc *TestClient, namespace string, podName string, containerName string, mountPath string, shouldExist bool) {
	readCmd := fmt.Sprintf("cat %s/testfile", mountPath)
	output := execCommandInPod(tc, namespace, podName, containerName, readCmd)

	if shouldExist {
		o.Expect(output).To(o.ContainSubstring("storage test"))
		e2e.Logf("Data exists and verified in pod %s\n", podName)
	} else {
		o.Expect(output).To(o.Or(o.ContainSubstring("No such file or directory"), o.BeEmpty()))
		e2e.Logf("Data does not exist as expected in pod %s\n", podName)
	}
}
