package e2e

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecOptions passed to ExecWithOptions
type ExecOptions struct {
	Command       []string
	Namespace     string
	PodName       string
	ContainerName string
	Stdin         io.Reader
	CaptureStdout bool
	CaptureStderr bool
	// If false, whitespace in std{err,out} will be removed.
	PreserveWhitespace bool
	Quiet              bool
}

func execute(method string, url *url.URL, stdin io.Reader, stdout, stderr io.Writer, tty bool) error {
	exec, err := remotecommand.NewSPDYExecutor(config, method, url)
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
}

// ExecWithOptions executes a command in the specified container,
// returning stdout, stderr and error. `options` allowed for
// additional parameters to be passed.
func ExecWithOptions(options ExecOptions) (string, string, error) {
	if !options.Quiet {
		fmt.Printf("ExecWithOptions %+v", options)
	}
	const tty = false

	fmt.Printf("ExecWithOptions: Client creation")
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(options.PodName).
		Namespace(options.Namespace).
		SubResource("exec").
		Param("container", options.ContainerName)

	req.VersionedParams(&k8sv1.PodExecOptions{
		Container: options.ContainerName,
		Command:   options.Command,
		Stdin:     options.Stdin != nil,
		Stdout:    options.CaptureStdout,
		Stderr:    options.CaptureStderr,
		TTY:       tty,
	}, GetParameterCodec())

	var stdout, stderr bytes.Buffer
	fmt.Printf("ExecWithOptions: execute(POST %s)", req.URL())
	err := execute("POST", req.URL(), options.Stdin, &stdout, &stderr, tty)
	if options.PreserveWhitespace {
		return stdout.String(), stderr.String(), err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func execCommandInContainerWithFullOutput(podName, containerName string, command []string) (string, string, error) {
	return ExecWithOptions(ExecOptions{
		Command:            command,
		Namespace:          testNamespace,
		PodName:            podName,
		ContainerName:      containerName,
		Stdin:              nil,
		CaptureStdout:      true,
		CaptureStderr:      true,
		PreserveWhitespace: false,
	})
}

// execCommandInContainer executes the command in container.
func execCommandInContainer(podName, containerName string, command string) error {
	cmd := []string{
		"sh",
		"-c",
		command,
	}
	_, stderr, err := execCommandInContainerWithFullOutput(podName, containerName, cmd)
	fmt.Printf("Exec stderr: %q", stderr)
	if err != nil {
		return fmt.Errorf("failed to execute command in pod %v, container %v: %v",
			podName, containerName, err)
	}
	return nil
}

// execCommandInPod executes the command in pod.
func execCommandInPod(pod *k8sv1.Pod, cmd string) error {
	gomega.Expect(pod.Spec.Containers).NotTo(gomega.BeEmpty())
	return execCommandInContainer(pod.Name, pod.Spec.Containers[0].Name, cmd)
}

// writeDataInPod writes the data to pod.
func writeDataInPod(pod *k8sv1.Pod, mode string) error {
	gomega.Expect(pod.Spec.Containers).NotTo(gomega.BeEmpty())
	var filePath string
	if mode == "file" {
		filePath = pod.Spec.Containers[0].VolumeMounts[0].MountPath + "/test"
	} else {
		filePath = pod.Spec.Containers[0].VolumeDevices[0].DevicePath
	}

	command := fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 status=none", filePath)
	err := execCommandInPod(pod, command)
	if err != nil {
		return err
	}
	return err
}

// validateDataInPod validates the data in snapshot-restore.
func validateDataInPod(pod *k8sv1.Pod, mode string) error {
	var filePath string
	if mode == "file" {
		filePath = pod.Spec.Containers[0].VolumeMounts[0].MountPath + "/test"
	} else {
		filePath = pod.Spec.Containers[0].VolumeDevices[0].DevicePath
	}
	err := execCommandInPod(pod, fmt.Sprintf("cat %s", filePath))
	if err != nil {
		return err
	}
	return err
}
