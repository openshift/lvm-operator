package qe_tests

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
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

//go:embed testdata/*.yaml
var templateFS embed.FS

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

func (lvm *lvmCluster) deleteSafely() {
	deleteLVMClusterSafely(lvm.name, lvm.namespace, lvm.deviceClassName)
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

			// Check if disk is mounted (catches Prometheus EBS and other CSI volumes
			// where blkid may return empty even though the device is in use)
			mountCmd := "mount | grep '/dev/" + diskName + " ' || true"
			logf("    Running: %s\n", mountCmd)
			mountOutput := execCommandInNode(tc, workerName, mountCmd)
			if strings.TrimSpace(mountOutput) != "" {
				logf("      [X] /dev/%s is MOUNTED: %s\n", diskName, strings.TrimSpace(mountOutput))
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

	// Devices ending with a digit (nvme0n1, mmcblk0, md0) use "p" separator for
	// partitions (e.g. nvme0n1p1), others (sda, vdb) don't (e.g. sda1)
	var partitionName string
	if len(diskName) > 0 && diskName[len(diskName)-1] >= '0' && diskName[len(diskName)-1] <= '9' {
		partitionName = diskName + "p1"
	} else {
		partitionName = diskName + "1"
	}
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
	// Devices ending with a digit (nvme0n1, mmcblk0, md0) use "p" separator for
	// partitions (e.g. nvme0n1p1), others (sda, vdb) don't (e.g. sda1)
	var partitionName, pvName string
	if len(disk) > 0 && disk[len(disk)-1] >= '0' && disk[len(disk)-1] <= '9' {
		partitionName = disk + "p1"
		pvName = diskName + "p1"
	} else {
		partitionName = disk + "1"
		pvName = diskName + "1"
	}

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

func deleteSpecifiedResource(resourceType string, name string, namespace string) {
	var cmd *exec.Cmd
	if namespace == "" {
		cmd = exec.Command("oc", "delete", resourceType, name, "--ignore-not-found")
	} else {
		cmd = exec.Command("oc", "delete", resourceType, name, "-n", namespace, "--ignore-not-found")
	}
	_, err := cmd.CombinedOutput()
	o.Expect(err).NotTo(o.HaveOccurred())
	checkResourcesNotExist(resourceType, name, namespace)
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
		return nil
	}

	logf("Deleting LVMCluster %s...\n", name)

	// Delete and let the controller handle VG cleanup (matches upstream pattern)
	cmd := exec.Command("oc", "delete", "lvmcluster", name, "-n", namespace, "--ignore-not-found", "--timeout=4m")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logf("Normal delete timed out or failed: %v, output: %s\n", err, string(output))
		// Fall back to removing finalizers
		removeLVMVolumeGroupNodeStatusFinalizers(namespace)
		removeLVMVolumeGroupFinalizers(deviceClassName, namespace)
		removeLVMClusterFinalizers(name, namespace)
	} else {
		logf("LVMCluster %s deleted successfully: %s\n", name, string(output))
	}

	// Wait for LVMCluster to be fully gone
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		exists, _ := resourceExists("lvmcluster", name, namespace)
		if !exists {
			logf("LVMCluster %s fully removed\n", name)
			return nil
		}
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timed out waiting for LVMCluster %s in namespace %s to be deleted", name, namespace)
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

func deleteLVMClusterSafely(name string, namespace string, deviceClassName string) {
	exists, _ := resourceExists("lvmcluster", name, namespace)
	if !exists {
		return
	}
	removeLVMClusterFinalizers(name, namespace)
	deleteSpecifiedResource("lvmcluster", name, namespace)
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

	// Sum raw bytes across all nodes first, then convert to GiB once to avoid
	// per-node truncation errors compounding on multi-node clusters.
	var totalBytes uint64 = 0
	for _, workerName := range workerNodes {
		cmd := "lsblk -b --output SIZE -n -d " + diskPath
		output := execCommandInNode(tc, workerName, cmd)
		if !strings.Contains(output, "not a block device") && output != "" {
			logf("Disk: %s found in worker node: %s\n", diskPath, workerName)
			bytes, parseErr := strconv.ParseUint(strings.TrimSpace(output), 10, 64)
			if parseErr == nil {
				totalBytes += bytes
			}
		}
	}
	const bytesPerGiB = 1024 * 1024 * 1024
	totalDiskSize := int(totalBytes / bytesPerGiB)
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

	// Use plain "oc apply" (matching upstream) so the validating webhook is invoked.
	// createLVMClusterFromJSON uses --server-side --force-conflicts which can bypass the webhook.
	cmd := exec.Command("oc", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(lvmClusterYAML)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
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
	listDeviceCmd := "echo $(lsblk -J -o NAME,FSTYPE,TYPE | jq -r '.blockdevices[] | select(.children == null and (.fstype == null or .fstype == \"LVM2_member\") and .type == \"disk\") | .name')"
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
	if len(lvm.optionalPaths) > 0 {
		pathsYAML += "        optionalPaths:\n"
		for _, path := range lvm.optionalPaths {
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
	paths, _, err := getLvmClusterPathsWithOptional(namespace)
	return paths, err
}

// getLvmClusterPathsWithOptional returns paths split into mandatory (common to all nodes)
// and optional (exist on some nodes but not all, or have non-LVM filesystem on some nodes).
// This handles MNO clusters where Prometheus EBS or asymmetric disk counts cause
// non-uniform device names across worker nodes.
func getLvmClusterPathsWithOptional(namespace string) ([]string, []string, error) {
	lvmClusterName, err := getLVMClusterName(namespace)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command("oc", "get", "lvmcluster", lvmClusterName, "-n", namespace,
		"-o=jsonpath={.status.deviceClassStatuses[*].nodeStatus[*].devices[*]}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get LVMCluster paths: %w, output: %s", err, string(output))
	}
	allPaths := strings.Fields(string(output))

	// Deduplicate paths
	seen := make(map[string]bool)
	var uniquePaths []string
	for _, path := range allPaths {
		if !seen[path] {
			seen[path] = true
			uniquePaths = append(uniquePaths, path)
		}
	}

	// Classify paths: check each path on all worker nodes
	// Paths valid on all nodes go to mandatory, others go to optional
	workerNodes, err := getWorkersList()
	if err != nil {
		logf("Warning: could not get worker list for path filtering, returning all as mandatory\n")
		return uniquePaths, nil, nil
	}

	var paths []string
	var optionalPaths []string
	for _, path := range uniquePaths {
		validOnAll := true
		validOnSome := false
		for _, node := range workerNodes {
			// Check device existence and filesystem type separately
			// so an unformatted device (blkid returns non-zero) is not misclassified as missing
			checkCmd := exec.Command("oc", "debug", "node/"+node, "--to-namespace=default", "--", "chroot", "/host", "bash", "-c",
				fmt.Sprintf("if test -b %s; then echo 'DEVICE_EXISTS'; blkid %s 2>/dev/null || true; else echo 'DEVICE_NOT_FOUND'; fi", path, path))
			checkOutput, err := checkCmd.CombinedOutput()
			outputStr := string(checkOutput)
			if err != nil {
				logf("Warning: oc debug failed for node %s path %s: %v, output: %s\n", node, path, err, outputStr)
				validOnAll = false
				continue
			}
			if strings.Contains(outputStr, "DEVICE_NOT_FOUND") {
				logf("Path %s: device does not exist on node %s\n", path, node)
				validOnAll = false
			} else if strings.Contains(outputStr, `TYPE="`) && !strings.Contains(outputStr, `TYPE="LVM2_member"`) {
				logf("Path %s: has non-LVM filesystem on node %s (not usable)\n", path, node)
				validOnAll = false
			} else {
				validOnSome = true
			}
		}
		if validOnAll {
			paths = append(paths, path)
		} else if validOnSome {
			optionalPaths = append(optionalPaths, path)
		}
		// If not valid on any node, skip entirely
	}

	logf("LVMCluster mandatory paths: %v, optional paths: %v\n", paths, optionalPaths)
	return paths, optionalPaths, nil
}

func (lvm *lvmCluster) createWithExportJSON(exportedJSON string) error {
	// Strip status from the exported JSON (matching upstream pattern using sjson.Delete)
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(exportedJSON), &obj); err != nil {
		return fmt.Errorf("failed to parse exported JSON: %w", err)
	}
	delete(obj, "status")
	if metadata, ok := obj["metadata"].(map[string]interface{}); ok {
		delete(metadata, "uid")
		delete(metadata, "resourceVersion")
		delete(metadata, "creationTimestamp")
		delete(metadata, "managedFields")
		delete(metadata, "generation")
		delete(metadata, "selfLink")
		delete(metadata, "ownerReferences")
	}

	cleanedJSON, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal cleaned JSON: %w", err)
	}

	// Use plain oc apply (no --server-side) matching upstream
	cmd := exec.Command("oc", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(string(cleanedJSON))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create LVMCluster from exported JSON: %w, output: %s", err, string(output))
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

// --- oc CLI helper functions using OpenShift Templates (aligned with upstream openshift-tests-private) ---

// readTemplate reads an embedded template file from the testdata directory.
func readTemplate(name string) (string, error) {
	data, err := templateFS.ReadFile("testdata/" + name)
	if err != nil {
		return "", fmt.Errorf("failed to read embedded template %s: %w", name, err)
	}
	return string(data), nil
}

// applyResourceFromTemplate processes an OpenShift template and applies the result.
// Mirrors upstream's applyResourceFromTemplateAsAdmin fallback path: performs manual
// parameter substitution instead of calling oc process, so it works without the
// Template API (e.g. MicroShift) and in CI where the namespace context is wrong.
//
// The templateName is the filename within testdata/ (e.g., "namespace-template.yaml").
// Remaining parameters follow the oc process convention: "-p", "KEY=VALUE", etc.
func applyResourceFromTemplate(templateName string, parameters ...string) error {
	templateContent, err := readTemplate(templateName)
	if err != nil {
		return err
	}

	// Extract -p KEY=VALUE pairs and perform manual substitution,
	// matching upstream's parameterizedTemplateByReplaceToFile fallback.
	processed := templateContent
	for i := 0; i < len(parameters); i++ {
		if parameters[i] == "-p" && i+1 < len(parameters) {
			i++
			parts := strings.SplitN(parameters[i], "=", 2)
			if len(parts) == 2 {
				// Replace ${{KEY}} (integer params) and ${KEY} (string params)
				processed = strings.ReplaceAll(processed, "${{"+parts[0]+"}}", parts[1])
				processed = strings.ReplaceAll(processed, "${"+parts[0]+"}", parts[1])
			}
		}
	}

	// Strip the Template wrapper — extract just the objects list as plain YAML.
	// The template YAML has metadata and parameters sections that oc apply doesn't understand.
	objectsIdx := strings.Index(processed, "\nobjects:\n")
	if objectsIdx == -1 {
		return fmt.Errorf("template %s has no 'objects:' section", templateName)
	}
	// Find where 'parameters:' starts (end of objects section)
	paramsIdx := strings.Index(processed, "\nparameters:\n")
	var objectsSection string
	if paramsIdx != -1 {
		objectsSection = processed[objectsIdx+len("\nobjects:\n") : paramsIdx]
	} else {
		objectsSection = processed[objectsIdx+len("\nobjects:\n"):]
	}

	// Remove the leading "- " list prefix and de-indent to produce valid YAML
	objectsSection = strings.TrimPrefix(objectsSection, "- ")
	objectsSection = strings.ReplaceAll(objectsSection, "\n  ", "\n")

	applyCmd := exec.Command("oc", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(objectsSection)
	applyOutput, err := applyCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply template %s: %w, output: %s", templateName, err, string(applyOutput))
	}
	return nil
}

// createNamespaceWithOC creates a namespace using oc process + oc apply with privileged PodSecurity labels
func createNamespaceWithOC(name string) error {
	err := applyResourceFromTemplate("namespace-template.yaml",
		"--ignore-unknown-parameters=true",
		"-p", "NAME="+name,
	)
	if err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", name, err)
	}
	logf("Created namespace: %s\n", name)
	return nil
}

// pvcConfig holds configuration for creating a PVC via oc CLI
type pvcConfig struct {
	name             string
	namespace        string
	storageClassName string
	accessMode       string // default "ReadWriteOnce"
	volumeMode       string // "Filesystem" or "Block", default "Filesystem"
	storage          string // e.g. "1Gi"
	dataSourceName   string // for clones or snapshot restores
	dataSourceKind   string // "PersistentVolumeClaim" or "VolumeSnapshot"
}

// createPVCWithOC creates a PVC using oc process + oc apply
func createPVCWithOC(cfg pvcConfig) error {
	if cfg.accessMode == "" {
		cfg.accessMode = "ReadWriteOnce"
	}
	if cfg.volumeMode == "" {
		cfg.volumeMode = "Filesystem"
	}
	if cfg.storage == "" {
		cfg.storage = "1Gi"
	}

	var templateName string
	params := []string{
		"--ignore-unknown-parameters=true",
		"-p", "PVCNAME=" + cfg.name,
		"-p", "PVCNAMESPACE=" + cfg.namespace,
		"-p", "SCNAME=" + cfg.storageClassName,
		"-p", "ACCESSMODE=" + cfg.accessMode,
		"-p", "VOLUMEMODE=" + cfg.volumeMode,
		"-p", "PVCCAPACITY=" + cfg.storage,
	}

	if cfg.dataSourceName != "" && cfg.dataSourceKind == "VolumeSnapshot" {
		templateName = "pvc-snapshot-restore-template.yaml"
		params = append(params, "-p", "DATASOURCENAME="+cfg.dataSourceName)
	} else if cfg.dataSourceName != "" {
		templateName = "pvc-clone-template.yaml"
		kind := cfg.dataSourceKind
		if kind == "" {
			kind = "PersistentVolumeClaim"
		}
		params = append(params, "-p", "DATASOURCEKIND="+kind, "-p", "DATASOURCENAME="+cfg.dataSourceName)
	} else {
		templateName = "pvc-template.yaml"
	}

	err := applyResourceFromTemplate(templateName, params...)
	if err != nil {
		return fmt.Errorf("failed to create PVC %s: %w", cfg.name, err)
	}
	logf("Created PVC: %s in namespace %s\n", cfg.name, cfg.namespace)
	return nil
}

// podConfig holds configuration for creating a Pod via oc CLI
type podConfig struct {
	name      string
	namespace string
	image     string
	pvcName   string
	mountPath string // for filesystem volumes
	isBlock   bool   // true = VolumeDevices, false = VolumeMounts
}

// createPodWithOC creates a Pod using oc process + oc apply with proper SecurityContext
func createPodWithOC(cfg podConfig) error {
	if cfg.image == "" {
		cfg.image = "registry.redhat.io/rhel8/support-tools:latest"
	}
	if cfg.mountPath == "" && !cfg.isBlock {
		cfg.mountPath = "/mnt/test"
	}

	volumeType := "volumeMounts"
	pathType := "mountPath"
	mountPath := cfg.mountPath
	if cfg.isBlock {
		volumeType = "volumeDevices"
		pathType = "devicePath"
		if mountPath == "" {
			mountPath = "/dev/dblock"
		}
	}

	err := applyResourceFromTemplate("pod-template.yaml",
		"--ignore-unknown-parameters=true",
		"-p", "PODNAME="+cfg.name,
		"-p", "PODNAMESPACE="+cfg.namespace,
		"-p", "PODIMAGE="+cfg.image,
		"-p", "PVCNAME="+cfg.pvcName,
		"-p", "VOLUMETYPE="+volumeType,
		"-p", "PATHTYPE="+pathType,
		"-p", "PODMOUNTPATH="+mountPath,
	)
	if err != nil {
		return fmt.Errorf("failed to create Pod %s: %w", cfg.name, err)
	}
	logf("Created Pod: %s in namespace %s\n", cfg.name, cfg.namespace)
	return nil
}

// deploymentConfig holds configuration for creating a Deployment via oc CLI
type deploymentConfig struct {
	name         string
	namespace    string
	replicas     int32
	image        string
	pvcName      string
	mountPath    string // for filesystem volumes
	isBlock      bool   // true = VolumeDevices, false = VolumeMounts
	nodeSelector string // optional: node hostname for pinning
}

// createDeploymentWithOC creates a Deployment using oc process + oc apply with proper SecurityContext
func createDeploymentWithOC(cfg deploymentConfig) error {
	if cfg.image == "" {
		cfg.image = "registry.redhat.io/rhel8/support-tools:latest"
	}
	if cfg.replicas == 0 {
		cfg.replicas = 1
	}
	if cfg.mountPath == "" && !cfg.isBlock {
		cfg.mountPath = "/mnt/test"
	}

	volumeType := "volumeMounts"
	typePath := "mountPath"
	mPath := cfg.mountPath
	if cfg.isBlock {
		volumeType = "volumeDevices"
		typePath = "devicePath"
		if mPath == "" {
			mPath = "/dev/dblock"
		}
	}

	var templateName string
	params := []string{
		"--ignore-unknown-parameters=true",
		"-p", "DNAME=" + cfg.name,
		"-p", "DNAMESPACE=" + cfg.namespace,
		"-p", fmt.Sprintf("REPLICASNUM=%d", cfg.replicas),
		"-p", "DLABEL=" + cfg.name,
		"-p", "MPATH=" + mPath,
		"-p", "PVCNAME=" + cfg.pvcName,
		"-p", "VOLUMETYPE=" + volumeType,
		"-p", "PATHTYPE=" + typePath,
		"-p", "PODIMAGE=" + cfg.image,
	}

	if cfg.nodeSelector != "" {
		templateName = "dep-with-nodeselector-template.yaml"
		params = append(params, "-p", "NODENAME="+cfg.nodeSelector)
	} else {
		templateName = "dep-template.yaml"
	}

	err := applyResourceFromTemplate(templateName, params...)
	if err != nil {
		return fmt.Errorf("failed to create Deployment %s: %w", cfg.name, err)
	}
	logf("Created Deployment: %s in namespace %s\n", cfg.name, cfg.namespace)
	return nil
}

func getPVCVolumeName(namespace string, pvcName string) string {
	cmd := exec.Command("oc", "get", "pvc", pvcName, "-n", namespace, "-o=jsonpath={.spec.volumeName}")
	output, err := cmd.CombinedOutput()
	o.Expect(err).NotTo(o.HaveOccurred())
	volumeName := strings.TrimSpace(string(output))
	logf("The PVC %s in namespace %s is bound to volume %s\n", pvcName, namespace, volumeName)
	return volumeName
}
