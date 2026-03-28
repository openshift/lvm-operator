package qe_tests

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

// TestClient provides a lightweight replacement for exutil.CLI
// It wraps controller-runtime client and provides similar functionality
// without the heavy openshift/origin dependency
type TestClient struct {
	client.Client
	Clientset *kubernetes.Clientset
	Config    *rest.Config
	namespace string
	testName  string
	ctx       context.Context
	asAdmin   bool
}

// CommandBuilder provides a fluent interface for building oc-like commands
type CommandBuilder struct {
	client       *TestClient
	verb         string
	args         []string
	withNs       bool
	useNamespace string
	inputStdin   string // Support for piping data to stdin
}

// NewTestClient creates a new TestClient similar to exutil.NewCLI
func NewTestClient(testName string) *TestClient {
	// Get kubeconfig from environment or default location
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		homeDir, _ := os.UserHomeDir()
		kubeconfig = homeDir + "/.kube/config"
	}

	// Load config
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		g.Fail(fmt.Sprintf("Failed to load kubeconfig: %v", err))
	}

	// Route Kubernetes API warnings through GinkgoWriter instead of stdout
	// This prevents warnings from polluting stdout and breaking OTE JSON parsing
	config.WarningHandler = rest.NewWarningWriter(g.GinkgoWriter, rest.WarningWriterOptions{})

	// Create scheme with all required types
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = lvmv1alpha1.AddToScheme(scheme)

	// Create controller-runtime client
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		g.Fail(fmt.Sprintf("Failed to create kubernetes client: %v", err))
	}

	// Create clientset for standard operations
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		g.Fail(fmt.Sprintf("Failed to create kubernetes clientset: %v", err))
	}

	return &TestClient{
		Client:    k8sClient,
		Clientset: clientset,
		Config:    config,
		testName:  testName,
		ctx:       context.Background(),
		asAdmin:   false,
	}
}

// Context returns the context used by this client
func (tc *TestClient) Context() context.Context {
	return tc.ctx
}

// AsAdmin returns a copy of the client configured for admin operations
func (tc *TestClient) AsAdmin() *TestClient {
	adminClient := *tc
	adminClient.asAdmin = true
	return &adminClient
}

// Namespace returns the current test namespace
func (tc *TestClient) Namespace() string {
	if tc.namespace == "" {
		// Generate a unique namespace name based on test name
		tc.namespace = fmt.Sprintf("e2e-test-%s-%d", tc.testName, time.Now().Unix())
	}
	return tc.namespace
}

// SetupProject creates a new namespace for the test
// Similar to exutil.CLI.SetupProject()
func (tc *TestClient) SetupProject() error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tc.Namespace(),
		},
	}

	if err := tc.Create(tc.ctx, ns); err != nil {
		logf("Failed to create namespace %s: %v", tc.Namespace(), err)
		return err
	}

	logf("Created test namespace: %s", tc.Namespace())
	tc.namespace = ns.Name
	return nil
}

// WithoutNamespace returns a command builder that operates without namespace context
func (tc *TestClient) WithoutNamespace() *TestClient {
	clientCopy := *tc
	clientCopy.namespace = ""
	return &clientCopy
}

// AdminKubeClient returns the kubernetes clientset
// Compatible with exutil.CLI.AdminKubeClient()
func (tc *TestClient) AdminKubeClient() kubernetes.Interface {
	return tc.Clientset
}

// Run starts building a command similar to oc run
// Example: tc.AsAdmin().Run("get").Args("pods").Output()
func (tc *TestClient) Run(verb string) *CommandBuilder {
	return &CommandBuilder{
		client:       tc,
		verb:         verb,
		args:         []string{},
		withNs:       tc.namespace != "",
		useNamespace: tc.namespace,
	}
}

// Args adds arguments to the command
func (cb *CommandBuilder) Args(args ...string) *CommandBuilder {
	cb.args = append(cb.args, args...)
	return cb
}

// Output executes the command and returns the output
func (cb *CommandBuilder) Output() (string, error) {
	return cb.execute()
}

// Execute runs the command without returning output
func (cb *CommandBuilder) Execute() error {
	_, err := cb.execute()
	return err
}

// execute builds and runs the oc command
func (cb *CommandBuilder) execute() (string, error) {
	cmdArgs := []string{cb.verb}
	cmdArgs = append(cmdArgs, cb.args...)

	if cb.withNs && !contains(cb.args, "-n") && !contains(cb.args, "--namespace") {
		cmdArgs = append(cmdArgs, "-n", cb.useNamespace)
	}

	cmd := exec.Command("oc", cmdArgs...)

	if cb.inputStdin != "" {
		cmd.Stdin = strings.NewReader(cb.inputStdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		logf("Command failed: oc %s\nStdout: %s\nStderr: %s\nError: %v",
			strings.Join(cmdArgs, " "), stdout.String(), stderr.String(), err)
		return stdout.String(), err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// Helper function to check if a slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// GetLVMCluster retrieves an LVMCluster by name using typed client
func (tc *TestClient) GetLVMCluster(name, namespace string) (*lvmv1alpha1.LVMCluster, error) {
	lvmCluster := &lvmv1alpha1.LVMCluster{}
	key := client.ObjectKey{Name: name, Namespace: namespace}

	if err := tc.Get(tc.ctx, key, lvmCluster); err != nil {
		return nil, err
	}

	return lvmCluster, nil
}

// ListLVMClusters lists all LVMClusters in a namespace
func (tc *TestClient) ListLVMClusters(namespace string) (*lvmv1alpha1.LVMClusterList, error) {
	lvmClusterList := &lvmv1alpha1.LVMClusterList{}

	listOpts := []client.ListOption{}
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	if err := tc.List(tc.ctx, lvmClusterList, listOpts...); err != nil {
		return nil, err
	}

	return lvmClusterList, nil
}

// DeleteNamespace deletes the test namespace
func (tc *TestClient) DeleteNamespace() error {
	if tc.namespace == "" {
		return nil
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tc.namespace,
		},
	}

	if err := tc.Delete(tc.ctx, ns); err != nil {
		logf("Failed to delete namespace %s: %v", tc.namespace, err)
		return err
	}

	logf("Deleted test namespace: %s", tc.namespace)
	return nil
}

// CreateFromYAML creates resources from a YAML manifest using oc apply
// This is useful for creating resources from templates
func (tc *TestClient) CreateFromYAML(yamlPath string) error {
	args := []string{"apply", "-f", yamlPath}

	if tc.namespace != "" {
		args = append(args, "-n", tc.namespace)
	}

	cmd := exec.Command("oc", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		logf("Failed to create from YAML %s: %v\nOutput: %s", yamlPath, err, string(output))
		return err
	}

	logf("Created resources from %s", yamlPath)
	return nil
}

// WaitForPodReady waits for a pod to be ready
func (tc *TestClient) WaitForPodReady(namespace, podName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(tc.ctx, timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod %s/%s to be ready", namespace, podName)
		default:
			pod := &corev1.Pod{}
			key := client.ObjectKey{Name: podName, Namespace: namespace}

			if err := tc.Get(tc.ctx, key, pod); err != nil {
				logf("Failed to get pod %s/%s: %v", namespace, podName, err)
				time.Sleep(5 * time.Second)
				continue
			}

			// Check if pod is ready
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					logf("Pod %s/%s is ready", namespace, podName)
					return nil
				}
			}

			time.Sleep(5 * time.Second)
		}
	}
}

// GetKubeConfig returns the rest.Config
func (tc *TestClient) GetKubeConfig() *rest.Config {
	return tc.Config
}

// SetNamespace sets the current namespace
func (tc *TestClient) SetNamespace(namespace string) {
	tc.namespace = namespace
}

// NewController creates a controller-runtime manager for testing
// Useful for testing reconciliation logic
func (tc *TestClient) NewController() (ctrl.Manager, error) {
	mgr, err := ctrl.NewManager(tc.Config, ctrl.Options{
		Scheme: tc.Scheme(),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	return mgr, nil
}
