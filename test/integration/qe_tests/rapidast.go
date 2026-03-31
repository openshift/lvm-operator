package qe_tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	RapidastJobTimeout = 5 * time.Minute
)

var (
	rapidastNamespace string
	rapidastBaseDir   string
)

func init() {
	// Get the directory where this test file is located, then append "rapidast" subdirectory
	_, currentFile, _, ok := runtime.Caller(0)
	if ok {
		rapidastBaseDir = filepath.Join(filepath.Dir(currentFile), "rapidast")
	} else {
		rapidastBaseDir = "qe_tests/rapidast"
	}
}

func setupRapidastTest() {
	rapidastNamespace = fmt.Sprintf("lvms-rapidast-%d", time.Now().UnixNano()%100000000)
	// Use plain "oc create" (matching upstream) instead of go client
	cmd := exec.Command("oc", "create", "namespace", rapidastNamespace)
	output, err := cmd.CombinedOutput()
	o.Expect(err).NotTo(o.HaveOccurred(), "Failed to create namespace: %s", strings.TrimSpace(string(output)))
	logf("Created rapidast test namespace: %s", rapidastNamespace)
}

func cleanupRapidastTest() {
	if rapidastNamespace == "" {
		return
	}

	logf("Starting cleanup for rapidast namespace: %s", rapidastNamespace)

	// Delete namespace using plain "oc delete" (matching upstream)
	cmd := exec.Command("oc", "delete", "namespace", rapidastNamespace, "--ignore-not-found")
	if output, err := cmd.CombinedOutput(); err != nil {
		logf("Warning: failed to delete namespace %s: %v, output: %s", rapidastNamespace, err, strings.TrimSpace(string(output)))
	}

	logf("Cleanup complete for rapidast namespace: %s", rapidastNamespace)
}

// addClusterRoleToSA adds cluster-admin role to the service account
func addClusterRoleToSA(ns, saName string) error {
	cmd := exec.Command("oc", "adm", "policy", "add-cluster-role-to-user", "cluster-admin",
		fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// removeClusterRoleFromSA removes cluster-admin role from the service account
func removeClusterRoleFromSA(ns, saName string) error {
	cmd := exec.Command("oc", "adm", "policy", "remove-cluster-role-from-user", "cluster-admin",
		fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// createRapidastConfigMap creates a ConfigMap with the rapidast config and policy files.
// The config contains a placeholder token that is replaced at runtime by the job
// using the projected serviceAccountToken volume.
func createRapidastConfigMap(ns, name, configFile, policyFile string) error {
	// Use plain "oc create" (matching upstream) - token placeholder is replaced at runtime by the job
	cmd := exec.Command("oc", "create", "configmap", name,
		"--from-file=rapidastconfig.yaml="+configFile,
		"--from-file=customscan.policy="+policyFile,
		"-n", ns)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// createRapidastJob creates the RapiDAST job from template
func createRapidastJob(ns, jobName, templateFile string) error {
	// Process the template
	cmd := exec.Command("oc", "process", "-f", templateFile, "-p", "NAME="+jobName)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to process template: %v", err)
	}

	// Apply the processed template
	applyCmd := exec.Command("oc", "apply", "-f", "-", "-n", ns)
	applyCmd.Stdin = strings.NewReader(string(output))
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("failed to apply job: %v", err)
	}

	return nil
}

// waitForRapidastJobCompletion waits for the job to complete
func waitForRapidastJobCompletion(ns, jobName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 30*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			// Use plain "oc get" (matching upstream) to check pod status
			cmd := exec.Command("oc", "get", "pods", "-n", ns, "-l", "job-name="+jobName,
				"-o=jsonpath={.items[0].metadata.name},{.items[0].status.phase},{.items[0].status.reason},{.items[0].status.message}")
			output, err := cmd.CombinedOutput()
			if err != nil {
				return false, nil // Pod may not exist yet
			}

			outputStr := strings.TrimSpace(string(output))
			if outputStr == "" {
				return false, nil // No pods found yet
			}

			parts := strings.SplitN(outputStr, ",", 4)
			if len(parts) < 2 {
				return false, nil
			}

			podName := parts[0]
			phase := parts[1]
			logf("RapiDAST Job pod status: %s", phase)

			switch phase {
			case "Succeeded":
				return true, nil
			case "Failed":
				reason := ""
				message := ""
				if len(parts) > 2 {
					reason = parts[2]
				}
				if len(parts) > 3 {
					message = parts[3]
				}
				return true, fmt.Errorf("job %s pod %s failed: reason=%s, message=%s", jobName, podName, reason, message)
			case "Pending", "Running":
				return false, nil
			default:
				return false, nil
			}
		})
}

// getRapidastJobLogs retrieves logs from the rapidast job pod
func getRapidastJobLogs(ns, jobName string) (string, error) {
	// Use plain "oc get" (matching upstream) to find pod name
	getPodCmd := exec.Command("oc", "get", "pods", "-n", ns, "-l", "job-name="+jobName,
		"-o=jsonpath={.items[0].metadata.name}")
	podNameOutput, err := getPodCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %v", err)
	}

	podName := strings.TrimSpace(string(podNameOutput))
	if podName == "" {
		return "", fmt.Errorf("no pods found for job %s", jobName)
	}

	// Use plain "oc logs" (matching upstream) to get logs
	logsCmd := exec.Command("oc", "logs", podName, "-n", ns)
	logsOutput, err := logsCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get pod logs: %v", err)
	}
	return string(logsOutput), nil
}

// saveRapidastResults saves the scan results to ARTIFACT_DIR
func saveRapidastResults(podLogs, apiGroupName string) {
	artifactDir := os.Getenv("ARTIFACT_DIR")
	if artifactDir == "" {
		logf("ARTIFACT_DIR not set, printing logs to stdout")
		logf("RapiDAST scan logs:\n%s", podLogs)
		return
	}

	// Create ARTIFACT_DIR if it doesn't exist
	rapidastResultsDir := filepath.Join(artifactDir, "rapidast_results_lvms")
	if err := os.MkdirAll(rapidastResultsDir, 0755); err != nil {
		logf("Failed to create results directory %s: %v", rapidastResultsDir, err)
		logf("RapiDAST scan logs:\n%s", podLogs)
		return
	}

	artifactFile := filepath.Join(rapidastResultsDir, apiGroupName+"_rapidast.result.txt")
	logf("Writing report to %s", artifactFile)

	if err := os.WriteFile(artifactFile, []byte(podLogs), 0644); err != nil {
		logf("Failed to write results file: %v", err)
		logf("RapiDAST scan logs:\n%s", podLogs)
	}
}

// parseRapidastResults parses the pod logs for risk levels
func parseRapidastResults(podLogs string) (riskHigh, riskMedium int) {
	podLogLines := strings.Split(podLogs, "\n")
	reHigh := regexp.MustCompile(`"riskdesc": .*High`)
	reMedium := regexp.MustCompile(`"riskdesc": .*Medium`)

	for _, line := range podLogLines {
		if reHigh.MatchString(line) {
			riskHigh++
		}
		if reMedium.MatchString(line) {
			riskMedium++
		}
	}

	return riskHigh, riskMedium
}

// rapidastScan runs a RapiDAST scan and returns true if no high-risk issues found
func rapidastScan(ns, configFile, scanPolicyFile, apiGroupName string) (bool, error) {
	jobName := fmt.Sprintf("rapidast-%d", time.Now().UnixNano()%100000000)

	g.By("Adding cluster-admin role to default SA")
	if err := addClusterRoleToSA(ns, "default"); err != nil {
		return false, fmt.Errorf("failed to add cluster-admin role: %w", err)
	}
	defer func() {
		if err := removeClusterRoleFromSA(ns, "default"); err != nil {
			logf("Warning: failed to remove cluster-admin role from SA default in namespace %s: %v", ns, err)
		}
	}()

	g.By("Creating ConfigMap with RapiDAST config")
	if err := createRapidastConfigMap(ns, jobName, configFile, scanPolicyFile); err != nil {
		logf("rapidastScan abort! create configmap failed: %v", err)
		logf("rapidast result: riskHigh=unknown riskMedium=unknown")
		return false, err
	}
	defer func() {
		// Use plain "oc delete" (matching upstream)
		cmd := exec.Command("oc", "delete", "configmap", jobName, "-n", ns, "--ignore-not-found")
		cmd.Run()
	}()

	g.By("Creating RapiDAST Job")
	jobTemplate := filepath.Join(rapidastBaseDir, "job_rapidast.yaml")
	if err := createRapidastJob(ns, jobName, jobTemplate); err != nil {
		logf("rapidastScan abort! create job failed: %v", err)
		logf("rapidast result: riskHigh=unknown riskMedium=unknown")
		return false, err
	}
	defer func() {
		// Use plain "oc delete" (matching upstream)
		cmd := exec.Command("oc", "delete", "job", jobName, "-n", ns, "--ignore-not-found")
		cmd.Run()
	}()

	g.By("Waiting for RapiDAST Job to complete")
	if err := waitForRapidastJobCompletion(ns, jobName, RapidastJobTimeout); err != nil {
		logf("rapidastScan abort! timeout waiting for job completion: %v", err)
		logf("rapidast result: riskHigh=unknown riskMedium=unknown")
		return false, err
	}

	g.By("Getting RapiDAST Job logs")
	podLogs, err := getRapidastJobLogs(ns, jobName)
	if err != nil {
		logf("rapidastScan abort! can not fetch logs: %v", err)
		logf("rapidast result: riskHigh=unknown riskMedium=unknown")
		return false, err
	}

	g.By("Saving results")
	saveRapidastResults(podLogs, apiGroupName)

	g.By("Parsing results for risk levels")
	riskHigh, riskMedium := parseRapidastResults(podLogs)
	logf("rapidast result: riskHigh=%d riskMedium=%d", riskHigh, riskMedium)

	if riskHigh > 0 {
		return false, fmt.Errorf("high risk alert found (%d), please check the scan result report", riskHigh)
	}
	return true, nil
}

var _ = g.Describe("[sig-storage] STORAGE", func() {

	g.It("Author:mmakwana-[OTP][LVMS] lvm.topolvm.io API should pass RapiDAST security scan", g.Label("SNO", "MNO", "Serial"), func() {
		setupRapidastTest()
		g.DeferCleanup(cleanupRapidastTest)

		g.By("Running RapiDAST scan against lvm.topolvm.io/v1alpha1 API")

		configFile := filepath.Join(rapidastBaseDir, "data_rapidastconfig_lvm_v1alpha1.yaml")
		policyFile := filepath.Join(rapidastBaseDir, "customscan.policy")

		// Verify files exist
		_, err := os.Stat(configFile)
		o.Expect(err).NotTo(o.HaveOccurred(), "Config file should exist: %s", configFile)

		_, err = os.Stat(policyFile)
		o.Expect(err).NotTo(o.HaveOccurred(), "Policy file should exist: %s", policyFile)

		// Run the scan
		passed, err := rapidastScan(rapidastNamespace, configFile, policyFile, "lvm.topolvm.io_v1alpha1")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(passed).To(o.BeTrue(), "RapiDAST scan should pass without high-risk findings")
	})
})
