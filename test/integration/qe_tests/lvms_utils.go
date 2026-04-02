package qe_tests

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

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

func logf(format string, args ...interface{}) {
	fmt.Fprintf(g.GinkgoWriter, format+"\n", args...)
}

func getRandomString() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	const length = 8
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

type lvmCluster struct {
	name             string
	namespace        string
	deviceClassName  string
	deviceClassName2 string
	fsType           string
	fsType2          string
	paths            []string
	optionalPaths    []string
	nodeSelector     *lvmNodeSelector
}

type lvmNodeSelector struct {
	key      string
	operator string
	values   []string
}

type lvmClusterOption func(*lvmCluster)

func setLvmClusterName(name string) lvmClusterOption {
	return func(lvm *lvmCluster) {
		lvm.name = name
	}
}

func setLvmClusterNamespace(namespace string) lvmClusterOption {
	return func(lvm *lvmCluster) {
		lvm.namespace = namespace
	}
}

func setLvmClusterDeviceClassName(deviceClassName string) lvmClusterOption {
	return func(lvm *lvmCluster) {
		lvm.deviceClassName = deviceClassName
	}
}

func setLvmClusterDeviceClassName2(deviceClassName2 string) lvmClusterOption {
	return func(lvm *lvmCluster) {
		lvm.deviceClassName2 = deviceClassName2
	}
}

func setLvmClusterFsType(fsType string) lvmClusterOption {
	return func(lvm *lvmCluster) {
		lvm.fsType = fsType
	}
}

func setLvmClusterPaths(paths []string) lvmClusterOption {
	return func(lvm *lvmCluster) {
		lvm.paths = paths
	}
}

func setLvmClusterOptionalPaths(optionalPaths []string) lvmClusterOption {
	return func(lvm *lvmCluster) {
		lvm.optionalPaths = optionalPaths
	}
}

func newLvmCluster(opts ...lvmClusterOption) *lvmCluster {
	lvm := &lvmCluster{
		name:            "test-lvmcluster",
		namespace:       "openshift-lvm-storage",
		deviceClassName: "vg1",
		fsType:          "xfs",
		fsType2:         "ext4",
		paths:           []string{},
		optionalPaths:   []string{},
	}
	for _, opt := range opts {
		opt(lvm)
	}
	return lvm
}

func (lvm *lvmCluster) createWithNodeSelector(key string, operator string, values []string) error {
	lvm.nodeSelector = &lvmNodeSelector{
		key:      key,
		operator: operator,
		values:   values,
	}
	return lvm.create()
}

func (lvm *lvmCluster) create() error {
	yaml := lvm.buildYAML()
	return createLVMClusterFromJSON(yaml)
}

func (lvm *lvmCluster) buildYAML() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: %s
  namespace: %s
spec:
  storage:
    deviceClasses:
    - name: %s
`, lvm.name, lvm.namespace, lvm.deviceClassName))

	// Add nodeSelector if present
	if lvm.nodeSelector != nil {
		sb.WriteString(`      nodeSelector:
        nodeSelectorTerms:
        - matchExpressions:
          - key: ` + lvm.nodeSelector.key + `
            operator: ` + lvm.nodeSelector.operator + `
            values:
`)
		for _, v := range lvm.nodeSelector.values {
			sb.WriteString(fmt.Sprintf("            - %s\n", v))
		}
	}

	sb.WriteString(`      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10
`)

	// Add deviceSelector if paths or optionalPaths are present
	if len(lvm.paths) > 0 || len(lvm.optionalPaths) > 0 {
		sb.WriteString("      deviceSelector:\n")
		if len(lvm.paths) > 0 && lvm.paths[0] != "" {
			sb.WriteString("        paths:\n")
			for _, p := range lvm.paths {
				if p != "" {
					sb.WriteString(fmt.Sprintf("        - %s\n", p))
				}
			}
		}
		if len(lvm.optionalPaths) > 0 && lvm.optionalPaths[0] != "" {
			sb.WriteString("        optionalPaths:\n")
			for _, p := range lvm.optionalPaths {
				if p != "" {
					sb.WriteString(fmt.Sprintf("        - %s\n", p))
				}
			}
		}
		sb.WriteString("        forceWipeDevicesAndDestroyAllData: true\n")
	}

	return sb.String()
}

func (lvm *lvmCluster) waitReady(timeout time.Duration) error {
	return waitForLVMClusterReady(lvm.name, lvm.namespace, timeout)
}

func (lvm *lvmCluster) deleteSafely() error {
	return deleteLVMClusterSafely(lvm.name, lvm.namespace, lvm.deviceClassName)
}

func (lvm *lvmCluster) createWithMultiDeviceClasses() error {
	// Creates LVMCluster with vg1 as thin provisioning and vg2 as thick provisioning
	// vg1: has thinPoolConfig (supports snapshots)
	// vg2: NO thinPoolConfig (thick provisioning, no snapshot support)
	lvmClusterYAML := fmt.Sprintf(`apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: %s
  namespace: %s
spec:
  storage:
    deviceClasses:
    - name: %s
      fstype: %s
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10
      deviceSelector:
        paths:
        - %s
        forceWipeDevicesAndDestroyAllData: true
    - name: %s
      fstype: %s
      deviceSelector:
        paths:
        - %s
        forceWipeDevicesAndDestroyAllData: true
`, lvm.name, lvm.namespace, lvm.deviceClassName, lvm.fsType, lvm.paths[0], lvm.deviceClassName2, lvm.fsType2, lvm.paths[1])

	logf("Creating LVMCluster with YAML:\n%s\n", lvmClusterYAML)
	return createLVMClusterFromJSON(lvmClusterYAML)
}

func (lvm *lvmCluster) createWithTwoDeviceClasses() error {
	lvmClusterYAML := fmt.Sprintf(`apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: %s
  namespace: %s
spec:
  storage:
    deviceClasses:
    - name: %s
      fstype: %s
      deviceSelector:
        paths:
        - %s
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10
        chunkSizeCalculationPolicy: Static
        metadataSizeCalculationPolicy: Host
    - name: %s
      fstype: %s
      deviceSelector:
        paths:
        - %s
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10
        chunkSizeCalculationPolicy: Static
        metadataSizeCalculationPolicy: Host
`, lvm.name, lvm.namespace, lvm.deviceClassName, lvm.fsType, lvm.paths[0], lvm.deviceClassName2, lvm.fsType, lvm.paths[1])

	logf("Creating LVMCluster with two device classes:\n%s\n", lvmClusterYAML)
	return createLVMClusterFromJSON(lvmClusterYAML)
}

func (lvm *lvmCluster) deleteSecondDeviceClass() error {
	patch := `[{"op": "remove", "path": "/spec/storage/deviceClasses/1"}]`
	cmd := exec.Command("oc", "patch", "lvmcluster", lvm.name, "-n", lvm.namespace, "--type=json", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete second device class: %w, output: %s", err, string(output))
	}
	logf("Removed second device class from LVMCluster %s\n", lvm.name)
	return nil
}
func (lvm *lvmCluster) createWithTwoPaths() error {
	lvmClusterYAML := fmt.Sprintf(`apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: %s
  namespace: %s
spec:
  storage:
    deviceClasses:
    - name: %s
      fstype: %s
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 30
        overprovisionRatio: 10
      deviceSelector:
        paths:
        - %s
        - %s
`, lvm.name, lvm.namespace, lvm.deviceClassName, lvm.fsType, lvm.paths[0], lvm.paths[1])

	logf("Creating LVMCluster with two paths:\n%s\n", lvmClusterYAML)
	return createLVMClusterFromJSON(lvmClusterYAML)
}

func (lvm *lvmCluster) patchDevicePath(paths []string) error {
	// Build JSON array for paths
	pathsJSON := "["
	for i, p := range paths {
		if i > 0 {
			pathsJSON += ","
		}
		pathsJSON += fmt.Sprintf(`"%s"`, p)
	}
	pathsJSON += "]"

	patch := fmt.Sprintf(`[{"op": "replace", "path": "/spec/storage/deviceClasses/0/deviceSelector/paths", "value": %s}]`, pathsJSON)
	cmd := exec.Command("oc", "patch", "lvmcluster", lvm.name, "-n", lvm.namespace, "--type=json", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to patch device paths: %w, output: %s", err, string(output))
	}
	logf("Patched LVMCluster %s with paths=%v\n", lvm.name, paths)
	return nil
}

func int32Ptr(i int32) *int32 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func getRandomNum(m int64, n int64) int64 {
	return rand.Int63n(n-m+1) + m
}

func getThinPoolSizeByVolumeGroup(tc *TestClient, volumeGroup string, thinPoolName string) int {
	cmd := "lvs --units g 2> /dev/null | grep " + volumeGroup + " | awk '{if ($1 == \"" + thinPoolName + "\") print $4;}'"

	// Get list of worker nodes
	nodes, err := tc.Clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/worker",
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	var totalThinPoolSize int = 0

	for _, node := range nodes.Items {
		output := execCommandInNode(tc, node.Name, cmd)
		o.Expect(output).NotTo(o.BeEmpty(), fmt.Sprintf("Failed to get thin pool size on node %s", node.Name))

		regexForNumbersOnly := regexp.MustCompile("[0-9.]+")
		matches := regexForNumbersOnly.FindAllString(output, -1)
		o.Expect(len(matches)).To(o.BeNumerically(">", 0), fmt.Sprintf("No numeric value found in output: %s", output))

		sizeVal := matches[0]
		sizeNum := strings.Split(sizeVal, ".")
		thinPoolSize, err := strconv.Atoi(sizeNum[0])
		o.Expect(err).NotTo(o.HaveOccurred())

		totalThinPoolSize = totalThinPoolSize + thinPoolSize
	}

	logf("Total thin Pool size in Gi from backend nodes: %d", totalThinPoolSize)
	return totalThinPoolSize
}

func execCommandInNode(tc *TestClient, nodeName string, command string) string {
	const maxRetries = 3
	var output string
	var err error

	// Determine the namespace for the debug pod, following the same pattern as
	// openshift-tests-private: check if the current namespace is Active on the
	// target cluster and fall back to "default" if it is not (e.g. when running
	// in CI where the kubeconfig context namespace does not exist on the target).
	debugNamespace := tc.Namespace()
	extraArgs := []string{}
	nsOut, nsErr := tc.AsAdmin().Run("get").Args("ns/"+debugNamespace, "-o=jsonpath={.status.phase}", "--ignore-not-found").Output()
	if nsOut != "Active" || nsErr != nil {
		debugNamespace = "default"
		extraArgs = append(extraArgs, "--to-namespace="+debugNamespace)
	}

	// Retry loop to handle transient failures (like reference repo)
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Use oc debug node with chroot to execute command on host
		// This matches the reference repo's execCommandInSpecificNode behavior
		args := append([]string{"debug", "node/" + nodeName}, extraArgs...)
		args = append(args, "--", "chroot", "/host", "/bin/bash", "-c", command)
		cmd := exec.Command("oc", args...)
		outputBytes, execErr := cmd.CombinedOutput()
		output = string(outputBytes)

		if execErr == nil {
			// Filter out warning messages (like reference repo does)
			lines := strings.Split(output, "\n")
			var filteredLines []string
			for _, line := range lines {
				// Skip warning lines and debug pod lifecycle messages
				lowerLine := strings.ToLower(line)
				if strings.HasPrefix(lowerLine, "warning:") ||
					strings.Contains(lowerLine, "starting pod") ||
					strings.Contains(lowerLine, "to use host binaries") ||
					strings.Contains(lowerLine, "removing debug pod") {
					continue
				}
				filteredLines = append(filteredLines, line)
			}
			output = strings.TrimSpace(strings.Join(filteredLines, "\n"))
			logf("Executed on node %s: %s\n", nodeName, command)
			return output
		}

		err = execErr
		logf("Failed to execute on node %s (attempt %d/%d): %v, output: %s\n", nodeName, attempt+1, maxRetries, err, output)

		// Exponential backoff: 10s, 20s, 30s (like reference repo)
		backoff := time.Duration((attempt+1)*10) * time.Second
		time.Sleep(backoff)
	}

	logf("Failed to execute command on node %s after %d attempts: %v\n", nodeName, maxRetries, err)
	return ""
}

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
		logf("Failed to create executor: %v\n", err)
		return ""
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		logf("Failed to execute command: %v, stderr: %s\n", err, stderr.String())
		return ""
	}

	return stdout.String()
}

func getWorkersList() ([]string, error) {
	cmd := exec.Command("oc", "get", "nodes", "-l", "node-role.kubernetes.io/worker", "-o=jsonpath={.items[*].metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker nodes: %w, output: %s", err, string(output))
	}
	workers := strings.Fields(string(output))
	return workers, nil
}

func getListOfFreeDisksFromWorkerNodes(tc *TestClient) (map[string]int64, error) {
	// Check for mock mode (for CI environments without real disks)
	if mockDisk := os.Getenv("LVMS_MOCK_FREE_DISK"); mockDisk != "" {
		logf("⚙ MOCK MODE: Using disk %s from LVMS_MOCK_FREE_DISK env variable\n", mockDisk)
		workerNodes, err := getWorkersList()
		if err != nil {
			return nil, err
		}
		logf("  Simulating disk %s available on all %d worker node(s)\n", mockDisk, len(workerNodes))
		return map[string]int64{mockDisk: int64(len(workerNodes))}, nil
	}

	freeDiskNamesCount := make(map[string]int64)
	workerNodes, err := getWorkersList()
	if err != nil {
		return nil, err
	}

	logf("[DISK DISCOVERY STARTED]")
	logf("Scanning for free disks on %d worker node(s)...\n", len(workerNodes))

	for _, workerName := range workerNodes {
		isDiskFound := false
		logf("\n[Node: %s]\n", workerName)
		logf("  Running: lsblk | grep disk | awk \"{print $1}\"\n")

		output := execCommandInNode(tc, workerName, `lsblk | grep disk | awk "{print \$1}"`)
		if output == "" {
			logf("  [WARN] WARNING: No disks found or lsblk command failed (empty output)\n")
			continue
		}

		logf("  Raw disk list:\n")
		diskList := strings.Split(output, "\n")
		for _, diskLine := range diskList {
			diskLine = strings.TrimSpace(diskLine)
			if diskLine != "" {
				logf("    - %s\n", diskLine)
			}
		}

		logf("  Checking disk availability with blkid:\n")
		for _, diskName := range diskList {
			diskName = strings.TrimSpace(diskName)
			if diskName == "" {
				continue
			}

			blkidCmd := "blkid /dev/" + diskName
			logf("    Running: %s\n", blkidCmd)
			output := execCommandInNode(tc, workerName, blkidCmd)

			// disks that are used by existing LVMCluster have TYPE='LVM' OR Unused free disk does not return any output
			if strings.Contains(output, "LVM") || len(strings.TrimSpace(output)) == 0 {
				freeDiskNamesCount[diskName] = freeDiskNamesCount[diskName] + 1
				isDiskFound = true // at least 1 required free disk found
				if output == "" {
					logf("      [OK] /dev/%s is FREE (no filesystem signature)\n", diskName)
				} else {
					logf("      [OK] /dev/%s is available (LVM-managed): %s\n", diskName, output)
				}
			} else {
				logf("      [X] /dev/%s is IN USE: %s\n", diskName, output)
			}
		}

		if !isDiskFound {
			logf("  [WARN] WARNING: Worker node %s does not have mandatory unused free block device/disk attached\n", workerName)
		}
	}

	logf("[DISK DISCOVERY SUMMARY]")
	if len(freeDiskNamesCount) == 0 {
		logf("  [X] NO FREE DISKS FOUND on any worker node\n")
	} else {
		logf("  Free disks found across nodes:\n")
		for disk, count := range freeDiskNamesCount {
			logf("    - /dev/%s: available on %d/%d nodes\n", disk, count, len(workerNodes))
		}
	}
	logf("[END DISK DISCOVERY]")

	return freeDiskNamesCount, nil
}

func createLogicalVolumeOnDisk(tc *TestClient, nodeHostName string, disk string, vgName string, lvName string) {
	diskName := "/dev/" + disk

	// Create LVM disk partition
	createPartitionCmd := "echo -e 'n\\np\\n1\\n\\n\\nw' | fdisk " + diskName
	_, err := execCommandInNodeWithError(tc, nodeHostName, createPartitionCmd)
	o.Expect(err).NotTo(o.HaveOccurred())

	partitionName := diskName + "p1"
	// Unmount the partition if it's mounted
	unmountCmd := "umount " + partitionName + " || true"
	_, err = execCommandInNodeWithError(tc, nodeHostName, unmountCmd)
	o.Expect(err).NotTo(o.HaveOccurred())

	// Create Physical Volume
	createPV := "pvcreate " + partitionName
	_, err = execCommandInNodeWithError(tc, nodeHostName, createPV)
	o.Expect(err).NotTo(o.HaveOccurred())

	// Create Volume Group
	createVG := "vgcreate " + vgName + " " + partitionName
	_, err = execCommandInNodeWithError(tc, nodeHostName, createVG)
	o.Expect(err).NotTo(o.HaveOccurred())

	// Create Logical Volume
	createLV := "lvcreate -n " + lvName + " -l 100%FREE " + vgName
	_, err = execCommandInNodeWithError(tc, nodeHostName, createLV)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func removeLogicalVolumeOnDisk(tc *TestClient, nodeHostName string, disk string, vgName string, lvName string) {
	diskName := "/dev/" + disk
	partitionName := disk + "p1"
	pvName := diskName + "p1"

	existsLV := `lvdisplay /dev/` + vgName + `/` + lvName + ` && echo "true" || echo "false"`
	outputLV, err := execCommandInNodeWithError(tc, nodeHostName, existsLV)
	o.Expect(err).NotTo(o.HaveOccurred())
	lvExists := strings.Contains(outputLV, "true")

	// If VG exists, proceed to check LV and remove accordingly
	existsVG := `vgdisplay | grep -q '` + vgName + `' && echo "true" || echo "false"`
	outputVG, err := execCommandInNodeWithError(tc, nodeHostName, existsVG)
	o.Expect(err).NotTo(o.HaveOccurred())
	if strings.Contains(outputVG, "true") {
		if lvExists {
			// Remove Logical Volume (LV)
			removeLV := "lvremove -f /dev/" + vgName + "/" + lvName
			_, err = execCommandInNodeWithError(tc, nodeHostName, removeLV)
			o.Expect(err).NotTo(o.HaveOccurred())
		}
		// Remove Volume Group (VG)
		removeVG := "vgremove -f " + vgName
		_, err = execCommandInNodeWithError(tc, nodeHostName, removeVG)
		o.Expect(err).NotTo(o.HaveOccurred())
	}

	existsPV := `pvdisplay | grep -q '` + pvName + `' && echo "true" || echo "false"`
	outputPV, err := execCommandInNodeWithError(tc, nodeHostName, existsPV)
	o.Expect(err).NotTo(o.HaveOccurred())
	if strings.Contains(outputPV, "true") {
		//Remove Physical Volume (PV)
		removePV := "pvremove -f " + pvName
		_, err = execCommandInNodeWithError(tc, nodeHostName, removePV)
		o.Expect(err).NotTo(o.HaveOccurred())
	}

	existsPartition := `lsblk | grep -q '` + partitionName + `' && echo "true" || echo "false"`
	outputPartition, err := execCommandInNodeWithError(tc, nodeHostName, existsPartition)
	o.Expect(err).NotTo(o.HaveOccurred())
	if strings.Contains(outputPartition, "true") {
		// Remove LVM disk partition
		removePartitionCmd := "echo -e 'd\\nw' | fdisk " + diskName
		_, err = execCommandInNodeWithError(tc, nodeHostName, removePartitionCmd)
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

func execCommandInNodeWithError(tc *TestClient, nodeName string, command string) (string, error) {
	output := execCommandInNode(tc, nodeName, command)
	if output == "" {
		return output, fmt.Errorf("command returned empty output")
	}
	return output, nil
}

func getLVMClusterJSON(name string, namespace string) (string, error) {
	cmd := exec.Command("oc", "get", "lvmcluster", name, "-n", namespace, "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get LVMCluster: %w, output: %s", err, string(output))
	}
	return string(output), nil
}

func deleteSpecifiedResource(resourceType string, name string, namespace string) error {
	// Issue delete with --wait=false to return immediately, then poll for deletion
	var cmd *exec.Cmd
	if namespace == "" {
		cmd = exec.Command("oc", "delete", resourceType, name, "--ignore-not-found", "--wait=false")
	} else {
		cmd = exec.Command("oc", "delete", resourceType, name, "-n", namespace, "--ignore-not-found", "--wait=false")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete %s %s: %w, output: %s", resourceType, name, err, string(output))
	}
	logf("Deleted %s %s: %s\n", resourceType, name, strings.TrimSpace(string(output)))

	// Wait for resource to be fully deleted
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		exists, _ := resourceExists(resourceType, name, namespace)
		if !exists {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s %s to be deleted", resourceType, name)
}

func checkDeploymentPodMountedVolumeCouldRW(tc *TestClient, namespace string, deploymentName string, mountPath string) (string, string) {
	// Get pod from deployment
	pods, err := tc.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deploymentName),
	})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
	podName := pods.Items[0].Name

	// Generate random filename and content
	filename := "testfile-" + getRandomString()
	content := "testdata-" + getRandomString()

	// Write test data
	writeCmd := fmt.Sprintf("echo '%s' > %s/%s && sync", content, mountPath, filename)
	execCommandInPod(tc, namespace, podName, "test-container", writeCmd)

	// Read and verify
	readCmd := fmt.Sprintf("cat %s/%s", mountPath, filename)
	output := execCommandInPod(tc, namespace, podName, "test-container", readCmd)
	o.Expect(output).To(o.ContainSubstring(content))

	return filename, content
}

func checkDeploymentPodMountedVolumeDataExist(tc *TestClient, namespace string, deploymentName string, mountPath string, filename string, content string) {
	// Get pod from deployment
	pods, err := tc.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deploymentName),
	})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
	podName := pods.Items[0].Name

	// Read and verify
	readCmd := fmt.Sprintf("cat %s/%s", mountPath, filename)
	output := execCommandInPod(tc, namespace, podName, "test-container", readCmd)
	o.Expect(output).To(o.ContainSubstring(content))
}

func waitPVVolSizeToGetResized(tc *TestClient, pvName string, targetSize resource.Quantity, timeout time.Duration) {
	o.Eventually(func() bool {
		pv, err := tc.Clientset.CoreV1().PersistentVolumes().Get(context.TODO(), pvName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		currentSize := pv.Spec.Capacity[corev1.ResourceStorage]
		return currentSize.Cmp(targetSize) >= 0
	}, timeout, 15*time.Second).Should(o.BeTrue(), "PV %s did not reach target size %s", pvName, targetSize.String())
}

func waitPVCResizeSuccess(tc *TestClient, namespace string, pvcName string, targetSize resource.Quantity, timeout time.Duration) {
	o.Eventually(func() bool {
		pvc, err := tc.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		currentSize := pvc.Status.Capacity[corev1.ResourceStorage]
		return currentSize.Cmp(targetSize) >= 0
	}, timeout, 10*time.Second).Should(o.BeTrue(), "PVC %s did not reach target size %s", pvcName, targetSize.String())
}

func describeLVMCluster(name string, namespace string) string {
	cmd := exec.Command("oc", "describe", "lvmcluster", name, "-n", namespace)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("failed to describe lvmcluster: %v", err)
	}
	return string(output)
}

func deleteLVMClusterWithCleanup(name string, namespace string, deviceClassName string) error {
	// Check if LVMCluster exists
	exists, err := resourceExists("lvmcluster", name, namespace)
	if err != nil {
		return fmt.Errorf("failed to check if LVMCluster exists: %w", err)
	}
	if !exists {
		logf("LVMCluster %s does not exist, skipping deletion\n", name)
		// Even if LVMCluster doesn't exist, clean up any orphaned VG state
		cleanupVGOnAllNodes(deviceClassName)
		return nil
	}

	logf("Deleting LVMCluster %s with full backend cleanup...\n", name)

	// Delete with --wait=true to let controller do full cleanup
	cmd := exec.Command("oc", "delete", "lvmcluster", name, "-n", namespace, "--timeout=4m")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If timeout, try to force delete by removing finalizers
		logf("Normal delete timed out, forcing deletion by removing finalizers: %v\n", err)
		removeLVMVolumeGroupNodeStatusFinalizers(namespace)
		removeLVMVolumeGroupFinalizers(deviceClassName, namespace)
		removeLVMClusterFinalizers(name, namespace)

		// Wait for deletion to complete
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			exists, _ := resourceExists("lvmcluster", name, namespace)
			if !exists {
				break
			}
			time.Sleep(5 * time.Second)
		}

		// Since we forced deletion, the controller didn't clean up the VG
		// We MUST clean it up manually
		logf("Finalizers removed, manually cleaning up VG on all nodes...\n")
		cleanupVGOnAllNodes(deviceClassName)
	} else {
		logf("LVMCluster %s deleted with cleanup: %s\n", name, string(output))
	}

	// Wait for VG to be removed from all nodes
	logf("Waiting for VG %s to be removed from all nodes...\n", deviceClassName)
	workerNodes, _ := getWorkersList()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		allCleaned := true
		for _, nodeName := range workerNodes {
			// Check if VG still exists on this node
			vgOutput := execCommandInNode(tc, nodeName, "vgs --noheadings -o vg_name")
			if strings.Contains(vgOutput, deviceClassName) {
				allCleaned = false
				break
			}
		}
		if allCleaned {
			logf("VG %s removed from all nodes\n", deviceClassName)
			return nil
		}
		time.Sleep(5 * time.Second)
	}

	// If VG still exists after timeout, force destroy it
	logf("Warning: VG %s still exists after timeout, forcing destruction...\n", deviceClassName)
	cleanupVGOnAllNodes(deviceClassName)

	return nil
}

func createLVMClusterFromJSON(jsonContent string) error {
	// Use --force-conflicts and --server-side to handle UID mismatches when restoring
	// from exported JSON (which contains old uid/resourceVersion)
	cmd := exec.Command("oc", "apply", "-f", "-", "--force-conflicts=true", "--server-side=true")
	cmd.Stdin = strings.NewReader(jsonContent)
	_, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback to regular apply if server-side apply fails
		cmd2 := exec.Command("oc", "apply", "-f", "-")
		cmd2.Stdin = strings.NewReader(jsonContent)
		output2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("failed to create LVMCluster: %w, output: %s", err2, string(output2))
		}
	}
	return nil
}

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

func waitForLVMClusterReady(name string, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("oc", "get", "lvmcluster", name, "-n", namespace, "-o=jsonpath={.status.state}")
		output, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) == "Ready" {
			logf("LVMCluster %s is Ready\n", name)
			return nil
		}
		logf("LVMCluster %s state: %s, waiting...\n", name, string(output))
		time.Sleep(5 * time.Second)
	}
	// On timeout, dump describe output for debugging (matches reference behavior)
	lvmClusterDesc := describeLVMCluster(name, namespace)
	logf("oc describe lvmcluster %s:\n%s\n", name, lvmClusterDesc)
	return fmt.Errorf("timeout waiting for LVMCluster %s to become Ready", name)
}

func removeLVMClusterFinalizers(name string, namespace string) error {
	patch := `{"metadata":{"finalizers":[]}}`
	cmd := exec.Command("oc", "patch", "lvmcluster", name, "-n", namespace, "--type=merge", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove LVMCluster finalizers: %w, output: %s", err, string(output))
	}
	return nil
}

func removeLVMVolumeGroupFinalizers(deviceClassName string, namespace string) error {
	// Check if resource exists first
	checkCmd := exec.Command("oc", "get", "lvmvolumegroup", deviceClassName, "-n", namespace, "--ignore-not-found", "-o=name")
	checkOutput, _ := checkCmd.CombinedOutput()
	if strings.TrimSpace(string(checkOutput)) == "" {
		return nil
	}

	patch := `{"metadata":{"finalizers":[]}}`
	cmd := exec.Command("oc", "patch", "lvmvolumegroup", deviceClassName, "-n", namespace, "--type=merge", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove LVMVolumeGroup finalizers: %w, output: %s", err, string(output))
	}
	return nil
}

func removeLVMVolumeGroupNodeStatusFinalizers(namespace string) error {
	workerNodes, err := getWorkersList()
	if err != nil {
		return err
	}

	for _, workerName := range workerNodes {
		// Check if resource exists first
		checkCmd := exec.Command("oc", "get", "lvmvolumegroupnodestatus", workerName, "-n", namespace, "--ignore-not-found", "-o=name")
		checkOutput, _ := checkCmd.CombinedOutput()
		if len(strings.TrimSpace(string(checkOutput))) == 0 {
			// Resource doesn't exist, skip patching
			continue
		}

		patch := `{"metadata":{"finalizers":[]}}`
		cmd := exec.Command("oc", "patch", "lvmvolumegroupnodestatus", workerName, "-n", namespace, "--type=merge", "-p", patch)
		output, err := cmd.CombinedOutput()
		if err != nil {
			logf("Warning: failed to remove finalizers from LVMVolumeGroupNodeStatus %s: %v, output: %s\n", workerName, err, string(output))
		}
	}
	return nil
}

func deleteLVMClusterSafely(name string, namespace string, deviceClassName string) error {
	// Check if LVMCluster exists
	exists, err := resourceExists("lvmcluster", name, namespace)
	if err != nil {
		return fmt.Errorf("failed to check if LVMCluster exists: %w", err)
	}
	if !exists {
		logf("LVMCluster %s does not exist, skipping deletion\n", name)
		return nil
	}

	logf("Deleting LVMCluster %s...\n", name)

	// Try normal delete with timeout
	cmd := exec.Command("oc", "delete", "lvmcluster", name, "-n", namespace, "--timeout=2m")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logf("Normal delete timed out or failed for %s: %v, removing finalizers...\n", name, err)

		// Remove finalizers to force deletion
		removeLVMVolumeGroupNodeStatusFinalizers(namespace)
		removeLVMVolumeGroupFinalizers(deviceClassName, namespace)
		removeLVMClusterFinalizers(name, namespace)

		// Wait for deletion to complete
		deadline := time.Now().Add(1 * time.Minute)
		for time.Now().Before(deadline) {
			exists, _ := resourceExists("lvmcluster", name, namespace)
			if !exists {
				logf("LVMCluster %s deleted after finalizer removal\n", name)
				break
			}
			time.Sleep(5 * time.Second)
		}

		// Clean up VG on all nodes since we bypassed controller cleanup
		cleanupVGOnAllNodes(deviceClassName)
	} else {
		logf("LVMCluster %s deleted successfully: %s\n", name, string(output))
	}

	// Wait for LVMCluster to be fully gone
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		exists, _ := resourceExists("lvmcluster", name, namespace)
		if !exists {
			return nil
		}
		time.Sleep(3 * time.Second)
	}

	return nil
}

func cleanupVGOnAllNodes(vgName string) {
	workerNodes, err := getWorkersList()
	if err != nil {
		logf("Warning: could not get worker nodes for VG cleanup: %v\n", err)
		return
	}

	for _, nodeName := range workerNodes {
		logf("Cleaning up VG %s on node %s\n", vgName, nodeName)

		// Use forceDestroyVG which handles partial/corrupted VG state
		forceDestroyVGOnNode(nodeName, vgName)
	}
}

func forceDestroyVGOnNode(nodeName string, vgName string) {
	cleanupScript := fmt.Sprintf(`
set -x

# Check if VG exists at all
if ! vgs %[1]s 2>/dev/null; then
    echo "VG %[1]s does not exist, nothing to clean"
    exit 0
fi

# Step 1: Handle missing PVs (partial VG state)
echo "Checking for missing PVs in VG %[1]s..."
if vgs %[1]s 2>&1 | grep -q "missing"; then
    echo "VG has missing PVs, running vgreduce --removemissing..."
    vgreduce --removemissing --force %[1]s 2>/dev/null || true
fi

# Step 2: Get list of all LVs in this VG
echo "Finding all LVs in VG %[1]s..."
lvs_list=$(lvs --noheadings -o lv_name %[1]s 2>/dev/null | tr -d ' ' || true)

# Step 3: CRITICAL - Clean up kubelet CSI mount directories FIRST
# These hold stale references that prevent DM device removal
echo "Cleaning up kubelet CSI mount directories..."
for mp in $(mount 2>/dev/null | grep "/dev/mapper/%[1]s-" | awk '{print $3}'); do
    echo "Found mount: $mp"
    # Extract pod UID from path like /var/lib/kubelet/pods/<uid>/volumes/...
    pod_dir=$(echo "$mp" | grep -oE '/var/lib/kubelet/pods/[^/]+' || true)
    if [ -n "$pod_dir" ]; then
        echo "Force unmounting $mp..."
        umount -f "$mp" 2>/dev/null || true
        umount -l "$mp" 2>/dev/null || true
        echo "Removing pod directory: $pod_dir"
        rm -rf "$pod_dir" 2>/dev/null || true
    fi
done

# Step 4: Kill any processes using VG devices
echo "Killing processes using %[1]s devices..."
for dm in $(dmsetup ls 2>/dev/null | grep "^%[1]s-" | awk '{print $1}'); do
    fuser -km /dev/mapper/$dm 2>/dev/null || true
done

# Step 5: Force lazy unmount any remaining mounts
echo "Force lazy unmount remaining mounts..."
mount 2>/dev/null | grep "/dev/mapper/%[1]s-" | awk '{print $3}' | while read mp; do
    umount -lf "$mp" 2>/dev/null || true
done

# Step 6: Remove DM devices FIRST (before LV removal)
# This is critical - if DM devices are busy, LV removal will fail
echo "Removing device-mapper entries (first pass)..."
for dm in $(dmsetup ls 2>/dev/null | grep "^%[1]s-" | awk '{print $1}'); do
    echo "Removing dm: $dm"
    dmsetup remove --deferred "$dm" 2>/dev/null || true
    dmsetup remove -f "$dm" 2>/dev/null || true
done

# Step 7: Deactivate ALL LVs in the VG
echo "Deactivating LVs..."
lvchange -an %[1]s 2>/dev/null || true

# Step 8: Remove ALL LVs
echo "Removing all LVs..."
for lv in $lvs_list; do
    echo "Removing LV $lv..."
    lvremove -ff %[1]s/$lv 2>/dev/null || true
done
lvremove -ff %[1]s 2>/dev/null || true

# Step 9: Get list of PVs before removing VG
echo "Finding PVs in VG %[1]s..."
pv_list=$(pvs --noheadings -o pv_name -S vg_name=%[1]s 2>/dev/null | tr -d ' ' || true)

# Step 10: Remove the VG
echo "Removing VG %[1]s..."
vgremove -ff %[1]s 2>/dev/null || true

# Step 11: Remove PVs and wipe signatures
echo "Removing PVs and wiping signatures..."
for pv in $pv_list; do
    echo "Removing PV $pv..."
    pvremove -ff "$pv" 2>/dev/null || true
    wipefs -a "$pv" 2>/dev/null || true
done

# Step 12: Final DM cleanup pass with retry
echo "Final device-mapper cleanup..."
for i in 1 2 3; do
    remaining=$(dmsetup ls 2>/dev/null | grep "^%[1]s-" | wc -l)
    if [ "$remaining" -eq 0 ]; then
        break
    fi
    echo "Retry $i: $remaining DM devices remaining..."
    for dm in $(dmsetup ls 2>/dev/null | grep "^%[1]s-" | awk '{print $1}'); do
        dmsetup remove -f "$dm" 2>/dev/null || true
    done
    sleep 1
done

# Step 13: Remove VG device directory
echo "Removing /dev/%[1]s directory..."
rm -rf /dev/%[1]s 2>/dev/null || true

if vgs %[1]s 2>/dev/null; then
    echo "WARNING: VG %[1]s still exists after cleanup!"
elif dmsetup ls 2>/dev/null | grep -q "^%[1]s-"; then
    echo "WARNING: DM devices for %[1]s still exist!"
    dmsetup ls 2>/dev/null | grep "^%[1]s-"
else
    echo "VG %[1]s successfully removed"
fi

echo "Cleanup completed"
`, vgName)

	output := execCommandInNode(tc, nodeName, cleanupScript)
	logf("VG cleanup output on %s: %s\n", nodeName, output)
}

func deleteLVMClusterForRecovery(name string, namespace string, deviceClassName string) error {
	logf("Initiating delete of LVMCluster %s (without waiting)...\n", name)
	cmd := exec.Command("oc", "delete", "lvmcluster", name, "-n", namespace, "--wait=false")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to initiate LVMCluster deletion: %w, output: %s", err, string(output))
	}

	// Step 2: Immediately remove finalizers (this prevents backend cleanup)
	logf("Removing finalizers to prevent backend VG cleanup for %s...\n", name)
	time.Sleep(2 * time.Second) // Small delay to let deletion start

	removeLVMClusterFinalizers(name, namespace)
	removeLVMVolumeGroupFinalizers(deviceClassName, namespace)
	removeLVMVolumeGroupNodeStatusFinalizers(namespace)

	// Step 3: Wait for LVMCluster to be fully deleted from Kubernetes
	logf("Waiting for LVMCluster %s to be deleted from Kubernetes...\n", name)
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		exists, err := resourceExists("lvmcluster", name, namespace)
		if err != nil {
			logf("Warning: failed to check if LVMCluster %s exists: %v\n", name, err)
		}
		if !exists {
			logf("LVMCluster %s deleted from Kubernetes (backend VG will remain for recovery)\n", name)
			return nil
		}
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for LVMCluster %s to be deleted", name)
}

func getLVMClusterName(namespace string) (string, error) {
	cmd := exec.Command("oc", "get", "lvmcluster", "-n", namespace, "-o=jsonpath={.items[0].metadata.name}")
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

func resourceExists(resourceType string, name string, namespace string) (bool, error) {
	cmd := exec.Command("oc", "get", resourceType, name, "-n", namespace, "--ignore-not-found", "-o=jsonpath={.metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to check if %s %s exists in namespace %s: %w, output: %s", resourceType, name, namespace, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func getLvmClusterPath(namespace string) (string, error) {
	currentLVMClusterName, err := getLVMClusterName(namespace)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("oc", "get", "lvmcluster", "-n", namespace, currentLVMClusterName, "-o=jsonpath={.status.deviceClassStatuses[*].nodeStatus[*].devices[*]}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get LVMCluster path: %w, output: %s", err, string(output))
	}
	selectedDisk := strings.TrimSpace(string(output))
	logf("Current LVM cluster path: %s\n", selectedDisk)
	return selectedDisk, nil
}

func patchMetadataSizeCalculationPolicyToStatic(name string, namespace string, metadataSize string) error {
	patch := fmt.Sprintf(`[
		{"op": "replace", "path": "/spec/storage/deviceClasses/0/thinPoolConfig/metadataSizeCalculationPolicy", "value": "Static"},
		{"op": "replace", "path": "/spec/storage/deviceClasses/0/thinPoolConfig/metadataSize", "value": "%s"}
	]`, metadataSize)

	cmd := exec.Command("oc", "patch", "lvmcluster", name, "-n", namespace, "--type=json", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to patch LVMCluster metadataSize: %w, output: %s", err, string(output))
	}
	logf("Patched LVMCluster %s with metadataSizeCalculationPolicy set to 'Static' and metadataSize to %s\n", name, metadataSize)
	return nil
}

func getLogicalVolumeSelectedNode(namespace string, pvcName string) (string, error) {
	cmd := exec.Command("oc", "get", "pvc", pvcName, "-n", namespace, "-o=jsonpath={.metadata.annotations.volume\\.kubernetes\\.io/selected-node}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get selected node for PVC %s: %w, output: %s", pvcName, err, string(output))
	}
	nodeName := strings.TrimSpace(string(output))
	logf("The nodename in namespace %s for pvc %s is %s\n", namespace, pvcName, nodeName)
	return nodeName, nil
}

func patchOverprovisionRatio(name string, namespace string, overprovisionRatio string) error {
	patch := fmt.Sprintf(`[
		{"op": "replace", "path": "/spec/storage/deviceClasses/0/thinPoolConfig/overprovisionRatio", "value": %s}
	]`, overprovisionRatio)

	cmd := exec.Command("oc", "patch", "lvmcluster", name, "-n", namespace, "--type=json", "-p", patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to patch LVMCluster overprovisionRatio: %w, output: %s", err, string(output))
	}
	return nil
}

func waitForVGManagerPodRunning(tc *TestClient, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pods, err := tc.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/component=vg-manager",
		})
		if err != nil {
			logf("Failed to list vg-manager pods: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(pods.Items) == 0 {
			logf("No vg-manager pods found, waiting...\n")
			time.Sleep(5 * time.Second)
			continue
		}

		allRunning := true
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				allRunning = false
				logf("vg-manager pod %s is in %s phase, waiting...\n", pod.Name, pod.Status.Phase)
				break
			}
		}

		if allRunning {
			logf("All vg-manager pods are Running\n")
			return nil
		}

		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for vg-manager pods to be Running")
}

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
	logf("The PVC %s volumesize is %s\n", pvcName, volumeSizeStr)
	return volumeSizeStr, nil
}

func checkVolumeBiggerThanDisk(tc *TestClient, pvcName string, pvcNamespace string, thinPoolSize int) {
	pvSize, err := getVolSizeFromPvc(tc, pvcName, pvcNamespace)
	o.Expect(err).NotTo(o.HaveOccurred())

	// Extract numeric value from size string (e.g., "10Gi" -> "10")
	regexForNumbersOnly := regexp.MustCompile("[0-9]+")
	pvSizeVal := regexForNumbersOnly.FindAllString(pvSize, -1)[0]
	pvSizeNum, err := strconv.Atoi(pvSizeVal)
	o.Expect(err).NotTo(o.HaveOccurred())

	logf("Persistent volume Size in Gi: %d\n", pvSizeNum)
	o.Expect(pvSizeNum > thinPoolSize).Should(o.BeTrue())
}

func writePodData(tc *TestClient, namespace string, podName string, containerName string, mountPath string) {
	writeCmd := fmt.Sprintf("echo 'storage test' > %s/testfile", mountPath)
	output := execCommandInPod(tc, namespace, podName, containerName, writeCmd)
	logf("Write command output: %s\n", output)

	syncCmd := fmt.Sprintf("sync -f %s/testfile", mountPath)
	output = execCommandInPod(tc, namespace, podName, containerName, syncCmd)
}

func getOverProvisionLimitByVolumeGroup(tc *TestClient, volumeGroup string, thinPoolName string) int {
	thinPoolSize := getThinPoolSizeByVolumeGroup(tc, volumeGroup, thinPoolName)

	// Get overprovision ratio from LVMCluster
	cmd := exec.Command("oc", "get", "lvmcluster", "-n", "openshift-lvm-storage",
		"-o=jsonpath={.items[0].spec.storage.deviceClasses[0].thinPoolConfig.overprovisionRatio}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logf("Failed to get overprovision ratio: %v, output: %s\n", err, string(output))
		return thinPoolSize * 10 // default ratio
	}

	opRatioStr := strings.TrimSpace(string(output))
	opRatio, err := strconv.Atoi(opRatioStr)
	if err != nil {
		logf("Failed to parse overprovision ratio: %v\n", err)
		return thinPoolSize * 10 // default ratio
	}

	limit := thinPoolSize * opRatio
	logf("Over-Provisioning Limit in Gi: %d (thinPoolSize=%d, opRatio=%d)\n", limit, thinPoolSize, opRatio)
	return limit
}

func getTotalDiskSizeOnAllWorkers(tc *TestClient, diskPath string) int {
	workerNodes, err := getWorkersList()
	if err != nil {
		logf("Failed to get worker nodes: %v\n", err)
		return 0
	}

	var totalDiskSize int = 0
	for _, workerName := range workerNodes {
		cmd := "lsblk -b --output SIZE -n -d " + diskPath
		output := execCommandInNode(tc, workerName, cmd)
		if !strings.Contains(output, "not a block device") && output != "" {
			logf("Disk: %s found in worker node: %s\n", diskPath, workerName)
			size := bytesToGiB(strings.TrimSpace(output))
			totalDiskSize = totalDiskSize + size
		}
	}
	logf("Total Disk size of %s is equals %d Gi\n", diskPath, totalDiskSize)
	return totalDiskSize
}

func bytesToGiB(bytesStr string) int {
	bytes, err := strconv.ParseUint(bytesStr, 10, 64)
	if err != nil {
		return 0
	}
	const bytesPerGiB = 1024 * 1024 * 1024
	return int(bytes / bytesPerGiB)
}

func getOverProvisionRatioAndSizePercent(volumeGroup string) (int, int) {
	// Get overprovision ratio
	cmd := exec.Command("oc", "get", "lvmcluster", "-n", "openshift-lvm-storage",
		"-o=jsonpath={.items[0].spec.storage.deviceClasses[0].thinPoolConfig.overprovisionRatio}")
	output, err := cmd.CombinedOutput()
	opRatio := 10 // default
	if err == nil {
		ratio, parseErr := strconv.Atoi(strings.TrimSpace(string(output)))
		if parseErr == nil {
			opRatio = ratio
		}
	}

	// Get size percent
	cmd = exec.Command("oc", "get", "lvmcluster", "-n", "openshift-lvm-storage",
		"-o=jsonpath={.items[0].spec.storage.deviceClasses[0].thinPoolConfig.sizePercent}")
	output, err = cmd.CombinedOutput()
	sizePercent := 90 // default
	if err == nil {
		percent, parseErr := strconv.Atoi(strings.TrimSpace(string(output)))
		if parseErr == nil {
			sizePercent = percent
		}
	}

	logf("Over-Provision Ratio: %d, Size-percent: %d\n", opRatio, sizePercent)
	return opRatio, sizePercent
}

func getCurrentTotalLvmStorageCapacityByStorageClass(storageClassName string) int {
	cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", "openshift-lvm-storage",
		"-o=jsonpath={.items[?(@.storageClassName==\""+storageClassName+"\")].capacity}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logf("Failed to get CSI storage capacity: %v\n", err)
		return 0
	}

	totalCapacity := 0
	storageCapacity := strings.TrimSpace(string(output))

	if len(storageCapacity) != 0 {
		capacityList := strings.Fields(storageCapacity)
		for _, capacity := range capacityList {
			numericOnlyRegex := regexp.MustCompile("[^0-9]+")
			size, parseErr := strconv.ParseInt(numericOnlyRegex.ReplaceAllString(capacity, ""), 10, 64)
			if parseErr == nil {
				totalCapacity = totalCapacity + int(size)
			}
		}
	}
	return totalCapacity
}

func createLVMClusterWithOnlyOptionalPaths(name string, namespace string, deviceClass string, optionalPaths []string) error {
	optionalPathsJSON := "["
	for i, p := range optionalPaths {
		if i > 0 {
			optionalPathsJSON += ","
		}
		optionalPathsJSON += fmt.Sprintf(`"%s"`, p)
	}
	optionalPathsJSON += "]"

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
        optionalPaths: %s
        forceWipeDevicesAndDestroyAllData: true
`, name, namespace, deviceClass, optionalPathsJSON)

	return createLVMClusterFromJSON(lvmClusterYAML)
}
func createLVMClusterWithPathsAndOptionalPaths(name string, namespace string, deviceClass string, paths []string, optionalPaths []string) error {
	pathsJSON := "["
	for i, p := range paths {
		if i > 0 {
			pathsJSON += ","
		}
		pathsJSON += fmt.Sprintf(`"%s"`, p)
	}
	pathsJSON += "]"
	optionalPathsJSON := "["
	for i, p := range optionalPaths {
		if i > 0 {
			optionalPathsJSON += ","
		}
		optionalPathsJSON += fmt.Sprintf(`"%s"`, p)
	}
	optionalPathsJSON += "]"

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
        paths: %s
        optionalPaths: %s
        forceWipeDevicesAndDestroyAllData: true
`, name, namespace, deviceClass, pathsJSON, optionalPathsJSON)

	return createLVMClusterFromJSON(lvmClusterYAML)
}

func checkPodDataExists(tc *TestClient, namespace string, podName string, containerName string, mountPath string, shouldExist bool) {
	readCmd := fmt.Sprintf("cat %s/testfile", mountPath)
	output := execCommandInPod(tc, namespace, podName, containerName, readCmd)

	if shouldExist {
		o.Expect(output).To(o.ContainSubstring("storage test"))
		logf("Data exists and verified in pod %s\n", podName)
	} else {
		o.Expect(output).To(o.Or(o.ContainSubstring("No such file or directory"), o.BeEmpty()))
		logf("Data does not exist as expected in pod %s\n", podName)
	}
}

func writeDataIntoRawBlockVolume(tc *TestClient, namespace string, podName string, containerName string, devicePath string) {
	logf("Writing data into Raw Block volume %s in pod %s\n", devicePath, podName)
	// First, zero out the beginning of the device
	ddCmd := fmt.Sprintf("/bin/dd if=/dev/null of=%s bs=512 count=1", devicePath)
	execCommandInPod(tc, namespace, podName, containerName, ddCmd)
	// Write test data
	writeCmd := fmt.Sprintf("echo 'storage test' > %s", devicePath)
	execCommandInPod(tc, namespace, podName, containerName, writeCmd)
	// Sync to ensure data is flushed to disk before any subsequent operations
	logf("Data written to raw block volume successfully\n")
}

func checkDataInRawBlockVolume(tc *TestClient, namespace string, podName string, containerName string, devicePath string) {
	logf("Checking data in Raw Block volume %s in pod %s\n", devicePath, podName)
	// Read the data from the block device
	ddCmd := fmt.Sprintf("/bin/dd if=%s of=/tmp/testfile bs=512 count=1", devicePath)
	execCommandInPod(tc, namespace, podName, containerName, ddCmd)
	// Verify the content
	catCmd := "cat /tmp/testfile"
	output := execCommandInPod(tc, namespace, podName, containerName, catCmd)
	o.Expect(output).To(o.ContainSubstring("storage test"))
	logf("Data verified in raw block volume successfully\n")
}

func writeDeploymentDataBlockType(tc *TestClient, namespace string, deploymentName string, devicePath string) {
	logf("Writing data to block device %s via deployment %s\n", devicePath, deploymentName)
	// Get pod from deployment
	pods, err := tc.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deploymentName),
	})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
	podName := pods.Items[0].Name

	// Zero out the beginning of the device (matches reference)
	ddCmd := fmt.Sprintf("/bin/dd if=/dev/null of=%s bs=512 count=1", devicePath)
	execCommandInPod(tc, namespace, podName, "test-container", ddCmd)

	// Write test data using "block-data" as per reference
	writeCmd := fmt.Sprintf("echo 'block-data' > %s", devicePath)
	execCommandInPod(tc, namespace, podName, "test-container", writeCmd)
	logf("Block data written successfully\n")
}

func checkDeploymentDataBlockType(tc *TestClient, namespace string, deploymentName string, devicePath string) {
	logf("Checking data in block device %s via deployment %s\n", devicePath, deploymentName)
	// Get pod from deployment
	pods, err := tc.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deploymentName),
	})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(pods.Items)).To(o.BeNumerically(">", 0))
	podName := pods.Items[0].Name

	// Read data from block device to temp file
	ddCmd := fmt.Sprintf("/bin/dd if=%s of=/tmp/testfile bs=512 count=1", devicePath)
	execCommandInPod(tc, namespace, podName, "test-container", ddCmd)

	// Verify the content matches "block-data"
	catCmd := "cat /tmp/testfile"
	output := execCommandInPod(tc, namespace, podName, "test-container", catCmd)
	o.Expect(output).To(o.ContainSubstring("block-data"))
	logf("Block data verified successfully\n")
}

func getUnusedBlockDevicesFromNode(tc *TestClient, nodeName string) (deviceList []string) {
	listDeviceCmd := "echo $(lsblk --fs --json | jq -r '.blockdevices[] | select(.children == null and .fstype == null) | .name')"
	output := execCommandInNode(tc, nodeName, listDeviceCmd)
	deviceList = strings.Fields(output)
	return deviceList
}

func getLVMSUsableDiskCountFromWorkerNodes(tc *TestClient) (map[string]int64, error) {
	freeWorkerDiskCount := make(map[string]int64)
	workerNodes, err := getWorkersList()
	if err != nil {
		return nil, err
	}
	for _, nodeName := range workerNodes {
		output := execCommandInNode(tc, nodeName, "lsblk | grep disk | awk '{print $1}'")
		diskList := strings.Fields(output)
		for _, diskName := range diskList {
			blkidOutput := execCommandInNode(tc, nodeName, "blkid /dev/"+diskName)
			// disks used by existing LVMCluster have TYPE='LVM' OR unused free disk does not return any output
			if strings.Contains(blkidOutput, "LVM") || len(strings.TrimSpace(blkidOutput)) == 0 {
				freeWorkerDiskCount[nodeName] = freeWorkerDiskCount[nodeName] + 1
			}
		}
	}
	return freeWorkerDiskCount, nil
}

func createRAIDLevel1Disk(tc *TestClient, nodeName string, raidDiskName string) {
	deviceList := getUnusedBlockDevicesFromNode(tc, nodeName)
	o.Expect(len(deviceList) < 2).NotTo(o.BeTrue(), "Worker node: "+nodeName+" doesn't have at least two unused block devices/disks")

	raidCreateCmd := "yes | mdadm --create /dev/" + raidDiskName + " --level=1 --raid-devices=2 --assume-clean " + "/dev/" + deviceList[0] + " " + "/dev/" + deviceList[1]
	checkRaidStatCmd := "cat /proc/mdstat"
	cmdOutput := execCommandInNode(tc, nodeName, raidCreateCmd)
	o.Expect(cmdOutput).To(o.ContainSubstring("mdadm: array /dev/" + raidDiskName + " started"))

	o.Eventually(func() string {
		raidState := execCommandInNode(tc, nodeName, checkRaidStatCmd)
		return raidState
	}, 120*time.Second, 10*time.Second).Should(o.ContainSubstring(raidDiskName + " : active raid1"))
}

func removeRAIDLevelDisk(tc *TestClient, nodeName string, raidDiskName string) {
	checkRaidStatCmd := "cat /proc/mdstat"

	// Find RAID member disks first (before any cleanup)
	var deviceList []string
	output := execCommandInNode(tc, nodeName, "lsblk | grep disk | awk '{print $1}'")
	diskList := strings.Fields(output)

	cmdOutput := execCommandInNode(tc, nodeName, checkRaidStatCmd)
	for _, diskName := range diskList {
		blkidOutput := execCommandInNode(tc, nodeName, "blkid /dev/"+diskName)
		if strings.Contains(blkidOutput, "raid_member") {
			if strings.Contains(cmdOutput, diskName) {
				deviceList = append(deviceList, "/dev/"+diskName)
			}
		}
		if len(deviceList) > 1 {
			break
		}
	}

	o.Expect(len(deviceList) < 2).NotTo(o.BeTrue(),
		fmt.Sprintf("Could not find 2 RAID member disks on %s (found %d)", nodeName, len(deviceList)))

	// Clean up any LVM on the RAID device before stopping the array
	// This is needed because deleteLVMClusterSafely removes finalizers, which skips VG cleanup
	logf("Cleaning up any LVM on /dev/%s before stopping RAID on %s\n", raidDiskName, nodeName)

	// Check if the RAID device is a PV and get its VG
	pvDisplayCmd := "pvs --noheadings -o vg_name /dev/" + raidDiskName + " 2>/dev/null || true"
	vgName := strings.TrimSpace(execCommandInNode(tc, nodeName, pvDisplayCmd))

	if vgName != "" {
		logf("Found VG '%s' on RAID device /dev/%s, cleaning up...\n", vgName, raidDiskName)

		// Deactivate all LVs in the VG
		lvChangeCmd := "lvchange -an " + vgName + " 2>/dev/null || true"
		execCommandInNode(tc, nodeName, lvChangeCmd)

		// Remove all LVs in the VG
		lvRemoveCmd := "lvremove -ff " + vgName + " 2>/dev/null || true"
		execCommandInNode(tc, nodeName, lvRemoveCmd)

		// Remove the VG
		vgRemoveCmd := "vgremove -ff " + vgName + " 2>/dev/null || true"
		execCommandInNode(tc, nodeName, vgRemoveCmd)

		// Remove the PV
		pvRemoveCmd := "pvremove -ff /dev/" + raidDiskName + " 2>/dev/null || true"
		execCommandInNode(tc, nodeName, pvRemoveCmd)

		logf("LVM cleanup completed for VG '%s' on node %s\n", vgName, nodeName)
	}

	// Sync filesystem buffers before stopping RAID to prevent data loss
	execCommandInNode(tc, nodeName, "sync")

	// Stop the RAID array
	raidStopCmd := "mdadm --stop /dev/" + raidDiskName
	stopOutput := execCommandInNode(tc, nodeName, raidStopCmd)

	// Verify stop was successful
	expectedStopMsg := "mdadm: stopped /dev/" + raidDiskName
	o.Expect(stopOutput).To(o.ContainSubstring(expectedStopMsg))

	// Zero superblocks on member disks
	raidCleanBlockCmd := "mdadm --zero-superblock " + deviceList[0] + " " + deviceList[1]
	execCommandInNode(tc, nodeName, raidCleanBlockCmd)

	// Verify RAID is removed
	o.Eventually(func() string {
		raidState := execCommandInNode(tc, nodeName, checkRaidStatCmd)
		return raidState
	}, 120*time.Second, 10*time.Second).ShouldNot(o.ContainSubstring(raidDiskName))

	logf("RAID disk /dev/%s removed successfully from node %s\n", raidDiskName, nodeName)
}

func (lvm *lvmCluster) getCurrentTotalLvmStorageCapacityByWorkerNode(workerNode string) int {
	var totalCapacity int = 0
	var storageCapacity string

	// Retry until capacity value is returned in 'Mi' format (like reference repo)
	// Wait up to 180 seconds with 5-second intervals
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("oc", "get", "csistoragecapacity", "-n", "openshift-lvm-storage",
			fmt.Sprintf("-o=jsonpath={.items[?(@.nodeTopology.matchLabels.topology\\.topolvm\\.io/node==\"%s\")].capacity}", workerNode))
		output, err := cmd.CombinedOutput()
		if err != nil {
			logf("Failed to get CSI storage capacity: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		storageCapacity = strings.TrimSpace(string(output))
		// LVMS storage capacity is always returned in 'Mi' unit when available
		if strings.Contains(storageCapacity, "Mi") {
			break
		}
		logf("Storage capacity for node %s: %s (waiting for Mi format...)\n", workerNode, storageCapacity)
		time.Sleep(5 * time.Second)
	}

	logf("Storage capacity for node %s: %s\n", workerNode, storageCapacity)

	if len(storageCapacity) != 0 {
		capacityList := strings.Fields(storageCapacity)
		for _, capacity := range capacityList {
			numericOnlyRegex := regexp.MustCompile("[^0-9]+")
			size, parseErr := strconv.ParseInt(numericOnlyRegex.ReplaceAllString(capacity, ""), 10, 64)
			if parseErr == nil {
				totalCapacity = totalCapacity + int(size)
			}
		}
	}
	return totalCapacity
}

func (lvm *lvmCluster) createWithoutThinPool() error {
	var pathsYAML string
	if len(lvm.paths) > 0 {
		pathsYAML = "        paths:\n"
		for _, path := range lvm.paths {
			if path != "" {
				pathsYAML += fmt.Sprintf("        - %s\n", path)
			}
		}
	}

	// Match reference template: includes fstype (defaults to xfs)
	fstype := "xfs"
	if lvm.fsType != "" {
		fstype = lvm.fsType
	}

	lvmClusterYAML := fmt.Sprintf(`apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: %s
  namespace: %s
spec:
  storage:
    deviceClasses:
    - name: %s
      fstype: %s
      deviceSelector:
%s`, lvm.name, lvm.namespace, lvm.deviceClassName, fstype, pathsYAML)

	logf("Creating thick-provisioned LVMCluster:\n%s\n", lvmClusterYAML)
	return createLVMClusterFromJSON(lvmClusterYAML)
}

func getLvmClusterPaths(namespace string) ([]string, error) {
	lvmClusterName, err := getLVMClusterName(namespace)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("oc", "get", "lvmcluster", lvmClusterName, "-n", namespace,
		"-o=jsonpath={.status.deviceClassStatuses[*].nodeStatus[*].devices[*]}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get LVMCluster paths: %w, output: %s", err, string(output))
	}
	allPaths := strings.Fields(string(output))

	// Deduplicate paths - in MNO, the same device paths appear on multiple nodes
	seen := make(map[string]bool)
	var paths []string
	for _, path := range allPaths {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}

	logf("LVMCluster device paths: %v\n", paths)
	return paths, nil
}

func (lvm *lvmCluster) createWithExportJSON(exportedJSON string) error {
	// Clean the exported JSON by removing status and metadata fields that shouldn't be reapplied
	// Use kubectl apply with --force-conflicts to handle any conflicts
	cmd := exec.Command("oc", "apply", "-f", "-", "--force-conflicts=true", "--server-side=true")
	cmd.Stdin = strings.NewReader(exportedJSON)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try without server-side apply
		cmd2 := exec.Command("oc", "apply", "-f", "-")
		cmd2.Stdin = strings.NewReader(exportedJSON)
		output2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("failed to create LVMCluster from exported JSON: %w, output: %s", err2, string(output2))
		}
	}
	logf("Created LVMCluster from exported JSON: %s\n", string(output))
	return nil
}

func checkLVMClusterAndVGManagerPodReady(tc *TestClient, lvm *lvmCluster) {
	// Check vg-manager pods are running
	o.Eventually(func() bool {
		pods, err := tc.Clientset.CoreV1().Pods(lvm.namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/component=vg-manager",
		})
		if err != nil || len(pods.Items) == 0 {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				return false
			}
		}
		return true
	}, PodReadyTimeout, 5*time.Second).Should(o.BeTrue())

	// Check LVMCluster is Ready
	o.Eventually(func() string {
		cmd := exec.Command("oc", "get", "lvmcluster", lvm.name, "-n", lvm.namespace,
			"-o=jsonpath={.status.state}")
		output, _ := cmd.CombinedOutput()
		return strings.TrimSpace(string(output))
	}, LVMClusterReadyTimeout, 5*time.Second).Should(o.Equal("Ready"))
}

func setDiskEncryptPassphrase(tc *TestClient, disk string, passphrase string, workerNodes []string) {
	diskName := "/dev/" + disk

	for _, workerName := range workerNodes {
		// Format the disk with LUKS using the passphrase
		cmd := "echo -e \"" + passphrase + "\\n" + passphrase + "\" | cryptsetup -y -v luksFormat " + diskName
		output := execCommandInNode(tc, workerName, cmd)
		o.Expect(strings.Contains(strings.ToLower(output), "error")).NotTo(o.BeTrue(),
			fmt.Sprintf("Failed to format disk with LUKS on %s: %s", workerName, output))
		// Verify the encrypted disk using the same passphrase
		openCmd := "echo '" + passphrase + "' | cryptsetup luksOpen " + diskName + " encrypted"
		output = execCommandInNode(tc, workerName, openCmd)
		o.Expect(strings.Contains(strings.ToLower(output), "error")).NotTo(o.BeTrue(),
			fmt.Sprintf("Failed to open LUKS volume on %s: %s", workerName, output))
	}
}

func wipeDiskEncryptPassphrase(tc *TestClient, disk string, workerNodes []string) {
	diskName := "/dev/" + disk

	for _, workerName := range workerNodes {
		// Close encrypted volume
		closeCmd := "cryptsetup luksClose encrypted"
		output := execCommandInNode(tc, workerName, closeCmd)
		o.Expect(strings.Contains(strings.ToLower(output), "error")).NotTo(o.BeTrue(),
			fmt.Sprintf("Failed to close LUKS volume on %s: %s", workerName, output))

		// Erase LUKS header
		eraseCmd := "echo 'YES' | cryptsetup luksErase " + diskName
		output = execCommandInNode(tc, workerName, eraseCmd)
		o.Expect(strings.Contains(strings.ToLower(output), "error")).NotTo(o.BeTrue(),
			fmt.Sprintf("Failed to erase LUKS header on %s: %s", workerName, output))

		// Wipe filesystem signatures
		wipeCmd := "wipefs -a " + diskName
		output = execCommandInNode(tc, workerName, wipeCmd)
		o.Expect(strings.Contains(strings.ToLower(output), "error")).NotTo(o.BeTrue(),
			fmt.Sprintf("Failed to wipe disk on %s: %s", workerName, output))
	}
}

func checkStorageclassExists(sc string) {
	cmd := exec.Command("oc", "get", "sc", sc, "-o=jsonpath={.metadata.name}")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		output, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) == sc {
			logf("storageClass %s is installed successfully\n", sc)
			return
		}
		logf("Get error to get the storageclass %v\n", sc)
		time.Sleep(5 * time.Second)
		cmd = exec.Command("oc", "get", "sc", sc, "-o=jsonpath={.metadata.name}")
	}
	o.Expect(fmt.Errorf("could not find the storageclass %v", sc)).NotTo(o.HaveOccurred())
}

func checkVolumeMountCmdContain(tc *TestClient, volumeName string, nodeName string, content string) {
	command := "mount | grep " + volumeName
	deadline := time.Now().Add(60 * time.Second)
	var lastMsg string
	for time.Now().Before(deadline) {
		msg := execCommandInNode(tc, nodeName, command)
		lastMsg = msg
		if strings.Contains(msg, content) {
			logf("Volume %s mount on node %s contains '%s'\n", volumeName, nodeName, content)
			return
		}
		logf("Checking volume mount, trying again... (output: %s)\n", msg)
		time.Sleep(10 * time.Second)
	}
	logf("Final mount output for volume %s: %s\n", volumeName, lastMsg)
	o.Expect(fmt.Errorf("check volume: \"%s\" mount in node : \"%s\" contains \"%s\" failed", volumeName, nodeName, content)).NotTo(o.HaveOccurred())
}

func getNodeNameByPod(namespace string, podName string) string {
	cmd := exec.Command("oc", "get", "pod", podName, "-n", namespace, "-o=jsonpath={.spec.nodeName}")
	output, err := cmd.CombinedOutput()
	o.Expect(err).NotTo(o.HaveOccurred())
	nodeName := strings.TrimSpace(string(output))
	logf("The nodename in namespace %s for pod %s is %s\n", namespace, podName, nodeName)
	return nodeName
}

func checkResourcesNotExist(resourceType string, resourceName string, namespace string) {
	var cmd *exec.Cmd
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if namespace != "" {
			cmd = exec.Command("oc", "get", resourceType, resourceName, "-n", namespace)
		} else {
			cmd = exec.Command("oc", "get", resourceType, resourceName)
		}
		output, _ := cmd.CombinedOutput()
		outputStr := string(output)
		if strings.Contains(outputStr, "not found") || strings.Contains(outputStr, "No resources found") {
			if namespace != "" {
				logf("No %s resource exists in the namespace %s\n", resourceType, namespace)
			} else {
				logf("No %s resource exists\n", resourceType)
			}
			return
		}
		time.Sleep(5 * time.Second)
	}
	o.Expect(fmt.Errorf("the resources %s still exists in the namespace %s", resourceType, namespace)).NotTo(o.HaveOccurred())
}

func getPVCVolumeName(namespace string, pvcName string) string {
	cmd := exec.Command("oc", "get", "pvc", pvcName, "-n", namespace, "-o=jsonpath={.spec.volumeName}")
	output, err := cmd.CombinedOutput()
	o.Expect(err).NotTo(o.HaveOccurred())
	volumeName := strings.TrimSpace(string(output))
	logf("The PVC %s in namespace %s is bound to volume %s\n", pvcName, namespace, volumeName)
	return volumeName
}
