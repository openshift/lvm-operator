package qe_tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	RapidastJobTimeout    = 5 * time.Minute
	lvmsDastGcsSecretPath = "/var/run/lvms-dast/gcs-secret"
)

var rapidastNamespace string

func setupRapidastTest() {
	rapidastNamespace = fmt.Sprintf("lvms-rapidast-%d", time.Now().UnixNano()%100000000)
	cmd := exec.Command("oc", "create", "namespace", rapidastNamespace)
	output, err := cmd.CombinedOutput()
	o.Expect(err).NotTo(o.HaveOccurred(), "Failed to create namespace: %s", strings.TrimSpace(string(output)))

	labelCmd := exec.Command("oc", "label", "namespace", rapidastNamespace,
		"security.openshift.io/scc.podSecurityLabelSync=false",
		"pod-security.kubernetes.io/enforce=privileged",
		"pod-security.kubernetes.io/audit=privileged",
		"pod-security.kubernetes.io/warn=privileged",
		"--overwrite")
	output, err = labelCmd.CombinedOutput()
	o.Expect(err).NotTo(o.HaveOccurred(), "Failed to label namespace: %s", strings.TrimSpace(string(output)))

	logf("Created rapidast test namespace: %s", rapidastNamespace)
}

func cleanupRapidastTest() {
	if rapidastNamespace == "" {
		return
	}

	logf("Starting cleanup for rapidast namespace: %s", rapidastNamespace)

	cmd := exec.Command("oc", "delete", "namespace", rapidastNamespace, "--ignore-not-found")
	if output, err := cmd.CombinedOutput(); err != nil {
		logf("Warning: failed to delete namespace %s: %v, output: %s", rapidastNamespace, err, strings.TrimSpace(string(output)))
	}

	logf("Cleanup complete for rapidast namespace: %s", rapidastNamespace)
}

func isARMCluster() bool {
	cmd := exec.Command("oc", "get", "nodes", "-o=jsonpath={.items[*].status.nodeInfo.architecture}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logf("Warning: failed to get node architecture: %v", err)
		return false
	}
	archs := strings.TrimSpace(string(output))
	return strings.Contains(archs, "arm64") || strings.Contains(archs, "aarch64")
}

func createRapidastSA(ns string) error {
	content, err := templateFS.ReadFile("testdata/rapidast_sa.yaml")
	if err != nil {
		return fmt.Errorf("failed to read SA template: %w", err)
	}
	processed := strings.ReplaceAll(string(content), "NAMESPACE_PLACEHOLDER", ns)

	cmd := exec.Command("oc", "apply", "-f", "-", "-n", ns)
	cmd.Stdin = strings.NewReader(processed)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func cleanupRapidastSA() {
	for _, crb := range []string{"rapidast-lvms-cluster-admin", "rapidast-lvms-scc-privileged"} {
		cmd := exec.Command("oc", "delete", "clusterrolebinding", crb, "--ignore-not-found")
		if output, err := cmd.CombinedOutput(); err != nil {
			logf("Warning: failed to delete clusterrolebinding %s: %v, output: %s", crb, err, strings.TrimSpace(string(output)))
		}
	}
}

func getOCPVersion() (string, error) {
	cmd := exec.Command("oc", "version", "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get OCP version: %w", err)
	}

	var versionInfo struct {
		OpenshiftVersion string `json:"openshiftVersion"`
	}
	if err := json.Unmarshal(output, &versionInfo); err != nil {
		return "", fmt.Errorf("failed to parse OCP version JSON: %w", err)
	}

	if versionInfo.OpenshiftVersion == "" {
		return "unknown", nil
	}

	parts := strings.Split(versionInfo.OpenshiftVersion, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1], nil
	}
	return versionInfo.OpenshiftVersion, nil
}

func createRapidastConfigMap(ns, name string) error {
	tokenCmd := exec.Command("oc", "create", "token", "rapidast-sa", "-n", ns)
	tokenOutput, err := tokenCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create SA token: %s: %w", strings.TrimSpace(string(tokenOutput)), err)
	}
	token := strings.TrimSpace(string(tokenOutput))

	ocpVersion, err := getOCPVersion()
	if err != nil {
		logf("Warning: could not get OCP version: %v, using 'unknown'", err)
		ocpVersion = "unknown"
	}
	logf("Detected OCP version: %s", ocpVersion)

	configContent, err := templateFS.ReadFile("testdata/rapidast_config_lvm_v1alpha1.yaml")
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	resolvedConfig := strings.ReplaceAll(string(configContent), "Bearer sha256~xxxxxxxx", "Bearer "+token)
	resolvedConfig = strings.ReplaceAll(resolvedConfig, "OCPVERSION_PLACEHOLDER", ocpVersion)

	tmpConfigFile, err := os.CreateTemp("", "rapidastconfig-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	defer os.Remove(tmpConfigFile.Name())
	if err := os.WriteFile(tmpConfigFile.Name(), []byte(resolvedConfig), 0600); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}

	policyContent, err := templateFS.ReadFile("testdata/rapidast_customscan.policy")
	if err != nil {
		return fmt.Errorf("failed to read policy file: %w", err)
	}
	tmpPolicyFile, err := os.CreateTemp("", "rapidast-policy-*.policy")
	if err != nil {
		return fmt.Errorf("failed to create temp policy file: %w", err)
	}
	defer os.Remove(tmpPolicyFile.Name())
	if err := os.WriteFile(tmpPolicyFile.Name(), policyContent, 0600); err != nil {
		return fmt.Errorf("failed to write temp policy: %w", err)
	}

	args := []string{"create", "configmap", name,
		"--from-file=rapidastconfig.yaml=" + tmpConfigFile.Name(),
		"--from-file=customscan.policy=" + tmpPolicyFile.Name(),
	}

	gcsKeyPath := ""
	if _, err := os.Stat(lvmsDastGcsSecretPath); err == nil {
		gcsKeyPath = lvmsDastGcsSecretPath
	} else if envPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			gcsKeyPath = envPath
		}
	}

	if gcsKeyPath != "" {
		logf("GCS key found at %s, adding to configmap for result upload", gcsKeyPath)
		args = append(args, "--from-file=dast-gcs-secret.json="+gcsKeyPath)
	} else {
		logf("GCS key not found, results will not be uploaded to GCS")
	}

	args = append(args, "-n", ns)
	cmd := exec.Command("oc", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func createRapidastJob(ns, jobName string) error {
	jobContent, err := templateFS.ReadFile("testdata/rapidast_job.yaml")
	if err != nil {
		return fmt.Errorf("failed to read job template: %w", err)
	}

	tmpJobFile, err := os.CreateTemp("", "rapidast-job-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp job file: %w", err)
	}
	defer os.Remove(tmpJobFile.Name())
	if err := os.WriteFile(tmpJobFile.Name(), jobContent, 0600); err != nil {
		return fmt.Errorf("failed to write temp job file: %w", err)
	}

	cmd := exec.Command("oc", "process", "-f", tmpJobFile.Name(), "-p", "NAME="+jobName, "-n", ns)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to process template: %s: %w", strings.TrimSpace(string(output)), err)
	}

	applyCmd := exec.Command("oc", "apply", "-f", "-", "-n", ns)
	applyCmd.Stdin = strings.NewReader(string(output))
	applyOutput, err := applyCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply job: %s: %w", strings.TrimSpace(string(applyOutput)), err)
	}

	return nil
}

func waitForRapidastJobCompletion(ns, jobName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 30*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("oc", "get", "pods", "-n", ns, "-l", "job-name="+jobName,
				"-o=jsonpath={.items[0].metadata.name},{.items[0].status.phase},{.items[0].status.reason},{.items[0].status.message}")
			output, err := cmd.CombinedOutput()
			if err != nil {
				return false, nil
			}

			outputStr := strings.TrimSpace(string(output))
			if outputStr == "" {
				return false, nil
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

func getRapidastJobLogs(ns, jobName string) (string, error) {
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

	logsCmd := exec.Command("oc", "logs", podName, "-n", ns)
	logsOutput, err := logsCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get pod logs: %v", err)
	}
	return string(logsOutput), nil
}

func saveRapidastResults(podLogs, apiGroupName string) {
	artifactDir := os.Getenv("ARTIFACT_DIR")
	if artifactDir == "" {
		logf("ARTIFACT_DIR not set, printing logs to stdout")
		logf("RapiDAST scan logs:\n%s", podLogs)
		return
	}

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

func rapidastScan(ns, apiGroupName string) (bool, error) {
	jobName := fmt.Sprintf("rapidast-%d", time.Now().UnixNano()%100000000)

	g.By("Creating dedicated ServiceAccount with cluster-admin role")
	if err := createRapidastSA(ns); err != nil {
		return false, fmt.Errorf("failed to create rapidast SA: %w", err)
	}
	defer cleanupRapidastSA()

	g.By("Creating ConfigMap with RapiDAST config")
	if err := createRapidastConfigMap(ns, jobName); err != nil {
		logf("rapidastScan abort! create configmap failed: %v", err)
		logf("rapidast result: riskHigh=unknown riskMedium=unknown")
		return false, err
	}
	defer func() {
		cmd := exec.Command("oc", "delete", "configmap", jobName, "-n", ns, "--ignore-not-found")
		if output, err := cmd.CombinedOutput(); err != nil {
			logf("Warning: failed to delete configmap %s: %v, output: %s", jobName, err, strings.TrimSpace(string(output)))
		}
	}()

	g.By("Creating RapiDAST Job")
	if err := createRapidastJob(ns, jobName); err != nil {
		logf("rapidastScan abort! create job failed: %v", err)
		logf("rapidast result: riskHigh=unknown riskMedium=unknown")
		return false, err
	}
	defer func() {
		cmd := exec.Command("oc", "delete", "job", jobName, "-n", ns, "--ignore-not-found")
		if output, err := cmd.CombinedOutput(); err != nil {
			logf("Warning: failed to delete job %s: %v, output: %s", jobName, err, strings.TrimSpace(string(output)))
		}
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
		if isARMCluster() {
			g.Skip("RapiDAST image does not support ARM architecture")
		}

		setupRapidastTest()
		g.DeferCleanup(cleanupRapidastTest)

		g.By("Running RapiDAST scan against lvm.topolvm.io/v1alpha1 API")

		passed, err := rapidastScan(rapidastNamespace, "lvm.topolvm.io_v1alpha1")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(passed).To(o.BeTrue(), "RapiDAST scan should pass without high-risk findings")
	})
})
