package lvms

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
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
func getThinPoolSizeByVolumeGroup(volumeGroup string, thinPoolName string) int {
	// Use lvs with specific VG/LV selection to avoid complex shell piping
	cmd := fmt.Sprintf("lvs --units g --noheadings -o lv_size %s/%s 2>/dev/null || echo 0", volumeGroup, thinPoolName)

	// Get list of worker nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/worker",
	})
	Expect(err).NotTo(HaveOccurred())

	var totalThinPoolSize int = 0

	for _, node := range nodes.Items {
		output := execCommandInNode(node.Name, cmd)
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

	GinkgoWriter.Printf("Total thin pool size in Gi from backend nodes: %d\n", totalThinPoolSize)
	return totalThinPoolSize
}

// execCommandInNode executes a command in a specific node using debug pod
func execCommandInNode(nodeName string, command string) string {
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

	_, err := clientset.CoreV1().Pods("default").Create(context.TODO(), debugPod, metav1.CreateOptions{})
	if err != nil {
		GinkgoWriter.Printf("Failed to create debug pod: %v\n", err)
		return ""
	}

	defer func() {
		_ = clientset.CoreV1().Pods("default").Delete(context.TODO(), debugPodName, metav1.DeleteOptions{})
	}()

	// Wait for pod to be running
	Eventually(func() bool {
		pod, err := clientset.CoreV1().Pods("default").Get(context.TODO(), debugPodName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		return pod.Status.Phase == corev1.PodRunning
	}, 2*time.Minute, 5*time.Second).Should(BeTrue())

	// Execute command in the pod
	wrappedCmd := fmt.Sprintf("chroot /host /bin/bash -c '%s'", command)
	output := execCommandInPod("default", debugPodName, "debug", wrappedCmd)

	return output
}

// execCommandInPod executes a command in a pod
func execCommandInPod(namespace, podName, containerName, command string) string {
	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homeDir(), ".kube", "config"))
	if err != nil {
		GinkgoWriter.Printf("Failed to build config: %v\n", err)
		return ""
	}

	req := clientset.CoreV1().RESTClient().Post().
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

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		GinkgoWriter.Printf("Failed to create executor: %v\n", err)
		return ""
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		GinkgoWriter.Printf("Failed to execute command: %v, stderr: %s\n", err, stderr.String())
		return ""
	}

	return strings.TrimSpace(stdout.String())
}
