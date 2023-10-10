/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-logr/zapr"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	configv1 "github.com/openshift/api/config/v1"
	routesv1 "github.com/openshift/api/route/v1"
	secv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	lvmv1 "github.com/openshift/lvm-operator/api/v1alpha1"
	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/pelletier/go-toml"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	ctrlZap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type Collector struct {
	TotalCPU   Metric
	CPUPerNode Metric
	TotalMEM   Metric
	MemPerNode Metric
	Metrics    map[string]ObjectMetric
}

type Metric struct {
	Min float64
	Max float64
	Avg float64
}

type ObjectMetric struct {
	Name string
	Type string
	Cpu  Metric
	Mem  Metric
}

type LvmUnitMetrics struct {
	PrometheusURL string
	token         func() string
	Start         time.Time
	End           time.Time
	Name          string
	Namespace     string
	WorkloadType  string
	Metrics       map[string]ObjectMetric
	Instances     int
	PVCLabels     map[string]string
	StorageClass  *v1.StorageClass
}

const (
	CpuQueryTemplatePod       = `sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{cluster="", namespace="{{.Namespace}}"} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (pod)`
	CpuQueryTemplateContainer = `sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{cluster="", namespace="{{.Namespace}}"} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (container)`
	MemQueryTemplatePod       = `sum(container_memory_working_set_bytes{cluster="", namespace="{{.Namespace}}",container!="", image!=""} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (pod)`
	MemQueryTemplateContainer = `sum(container_memory_working_set_bytes{cluster="", namespace="{{.Namespace}}",container!="", image!=""} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (container)`
	Mebibyte                  = 1048576 // bytes
)

func init() {
	utilruntime.Must(k8sscheme.AddToScheme(scheme.Scheme))
	utilruntime.Must(lvmv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(snapapi.AddToScheme(scheme.Scheme))
	utilruntime.Must(secv1.Install(scheme.Scheme))
	utilruntime.Must(configv1.Install(scheme.Scheme))
	utilruntime.Must(routesv1.Install(scheme.Scheme))
}

type PerfTest struct {
	Scheme *runtime.Scheme
	client.Client

	// flags
	Instances            int
	TestNamespace        string
	TestStorageClassName string
	StorageClass         *v1.StorageClass
	NamePattern          string
	Token                string

	// variables
	PrometheusURL string
	PVCLabels     map[string]string
}

func main() {
	core := zapcore.NewCore(
		&ctrlZap.KubeAwareEncoder{Encoder: zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())},
		zapcore.AddSync(os.Stdout),
		zap.NewAtomicLevelAt(zapcore.Level(-9)),
	)
	zapLog := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	logr := zapr.NewLogger(zapLog.With(zap.Namespace("context")))
	ctrl.SetLogger(logr)

	logger := ctrl.Log.WithName("setup")
	if err := NewCmd(&PerfTest{}).Execute(); err != nil {
		logger.Error(err, "fatal error encountered")
		os.Exit(1)
	}
}

// NewCmd creates a new CLI command
func NewCmd(perfTest *PerfTest) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "operf",
		Short: "Commands for running LVMS Performance Tests",
		Long: `Usage of operf:
This script retrieves cpu and memory usage metrics for all the workloads created by the logical volume manager storage (lvms).

	It creates <instances> of "busybox" pods using PVCs provisioned via the Storage class provided
	Example:
	# go run test/performance/operf.go -token sha256~cj81ClyUYu7g05y8K-uLWm2AbrKTbNEQ96hEJcWStQo -sc lvms-vg1 -instances 16
`,
		SilenceErrors: false,
		SilenceUsage:  true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			log.FromContext(cmd.Context()).Info("initializing performance test")
			if perfTest.Token == "" {
				return fmt.Errorf("token for metrics extraction out of prometheus was not supplied")
			}

			kubeconfig, ok := os.LookupEnv("KUBECONFIG")
			if !ok {
				return fmt.Errorf("KUBECONFIG env var not set")
			}
			config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				return err
			}
			clnt, err := client.New(config, client.Options{Scheme: scheme.Scheme})
			if err != nil {
				return err
			}
			perfTest.Client = clnt

			route := &routesv1.Route{}
			if err := perfTest.Get(cmd.Context(), client.ObjectKey{
				Namespace: "openshift-monitoring",
				Name:      "prometheus-k8s",
			}, route); err != nil {
				return fmt.Errorf("cannot get prometheus route: %w", err)
			}
			perfTest.PrometheusURL = fmt.Sprintf("https://%s", route.Spec.Host)

			sc := &v1.StorageClass{}
			if err := perfTest.Get(cmd.Context(), client.ObjectKey{
				Name: perfTest.TestStorageClassName,
			}, sc); err != nil {
				return fmt.Errorf("cannot get storage class: %w", err)
			}
			sc.SetManagedFields(nil)
			perfTest.StorageClass = sc

			perfTest.PVCLabels = map[string]string{"app": "testPerformance"}
			log.FromContext(cmd.Context()).Info("initialization complete")
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(log.IntoContext(cmd.Context(), ctrl.Log.WithName("performance-test")), perfTest)
		},
	}

	cmd.Flags().IntVarP(&perfTest.Instances, "instances", "i", 4, "Number of Pods/Pvcs (each Pod uses a different PVC) to create in the test")
	cmd.Flags().StringVarP(&perfTest.TestNamespace, "namespace", "n", "openshift-storage", "Namespace where operator is deployed and the PVCs and test pods will be deployed/undeployed")
	cmd.Flags().StringVarP(&perfTest.TestStorageClassName, "storage-class", "s", "lvms-vg1", "Name of the topolvm storage class that will be used in the PVCs")
	cmd.Flags().StringVarP(&perfTest.NamePattern, "pattern", "p", "operf", "Pattern used to build the PVCs/Pods names")
	cmd.Flags().StringVarP(&perfTest.Token, "token", "t", "", "authentication token needed to connect with the Openshift cluster, you can also set an environment variable. export TOKEN=$(oc whoami -t)")

	return cmd
}

func run(ctx context.Context, perfTest *PerfTest) error {
	// Execute actions
	start, end, err := perfTest.run(ctx)

	if err != nil {
		return fmt.Errorf("usage tests encountered an error (start %s, end %s): %w",
			start, end, err)
	}

	tokenFn := func() string { return perfTest.Token }

	createUnit := func(name, workloadType string) LvmUnitMetrics {
		return LvmUnitMetrics{
			StorageClass:  perfTest.StorageClass,
			PVCLabels:     perfTest.PVCLabels,
			Instances:     perfTest.Instances,
			PrometheusURL: perfTest.PrometheusURL,
			token:         tokenFn,
			Namespace:     perfTest.TestNamespace,
			Name:          name,
			WorkloadType:  workloadType,
		}
	}

	// Retrieve and print metrics
	units := []LvmUnitMetrics{
		createUnit("lvms-operator", "deployment"),
		createUnit("topolvm-controller", "deployment"),
		createUnit("topolvm-node", "daemonset"),
		createUnit("vg-manager", "daemonset"),
	}
	collector := Collector{Metrics: make(map[string]ObjectMetric)}
	var errs []error
	for _, unit := range units {
		if err := unit.getAllMetrics(
			log.IntoContext(ctx, log.FromContext(ctx).WithValues("unit", unit.Name)),
			start, end,
			collector,
		); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	for _, v := range collector.Metrics {
		collector.TotalCPU.Min += v.Cpu.Min
		collector.TotalCPU.Avg += v.Cpu.Avg
		collector.TotalCPU.Max += v.Cpu.Max
		collector.TotalMEM.Min += v.Mem.Min
		collector.TotalMEM.Avg += v.Mem.Avg
		collector.TotalMEM.Max += v.Mem.Max
	}

	nodes := &corev1.NodeList{}
	if err := perfTest.List(ctx, nodes, client.HasLabels{"node-role.kubernetes.io/worker"}); err != nil {
		log.FromContext(ctx).Error(fmt.Errorf("cannot get node list for avg-per-node: %w", err), "failed to calculate average per node")
	} else {
		collector.MemPerNode.Min = collector.TotalMEM.Min / float64(len(nodes.Items))
		collector.MemPerNode.Avg = collector.TotalMEM.Avg / float64(len(nodes.Items))
		collector.MemPerNode.Max = collector.TotalMEM.Max / float64(len(nodes.Items))
		collector.CPUPerNode.Min = collector.TotalCPU.Min / float64(len(nodes.Items))
		collector.CPUPerNode.Avg = collector.TotalCPU.Avg / float64(len(nodes.Items))
		collector.CPUPerNode.Max = collector.TotalCPU.Max / float64(len(nodes.Items))
	}

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot open working directory: %w", err)
	}
	if file, err := os.Create(filepath.Join(dir, fmt.Sprintf("metrics-%s-%v-%v.toml", "summary", start.Unix(), end.Unix()))); err != nil {
		return fmt.Errorf("could not create metrics report file: %w", err)
	} else {
		if err := toml.NewEncoder(file).Encode(collector); err != nil {
			return fmt.Errorf("could not write metrics report file: %w", err)
		}
		log.FromContext(ctx).Info("container metrics written", "file", file.Name())
	}

	return nil
}

// run the create, delete tests
func (perfTest *PerfTest) run(ctx context.Context) (time.Time, time.Time, error) {
	logger := log.FromContext(ctx).WithValues(
		"instances", perfTest.Instances,
		"namespace", perfTest.TestNamespace,
		"storage-class", perfTest.TestStorageClassName)
	tsStartTest := time.Now()
	logger.Info("creating pvcs")
	if err := perfTest.createPVCs(ctx); err != nil {
		return tsStartTest, time.Now(), err
	}
	// Create PVCs
	logger.Info("pvcs created")
	tsPVCSCreated := time.Now()

	// Use PVCs
	logger.Info("creating pods")
	if err := perfTest.createPods(ctx); err != nil {
		return tsStartTest, time.Now(), err
	}
	logger.Info("pods created")
	tsPVCSUsed := time.Now()

	// Wait for Pods Running
	if err := perfTest.waitForPodsRunning(ctx); err != nil {
		return tsStartTest, time.Now(), fmt.Errorf("failed to wait for running pods: %w", err)
	}
	logger.Info("all pods are running")

	// Free PVCs
	logger.Info("deleting pods")
	if err := perfTest.deletePods(ctx); err != nil {
		return tsStartTest, time.Now(), fmt.Errorf("failed to delete test pods: %w", err)
	} else {
		logger.Info("all pods deleted")
	}
	tsPVCSFree := time.Now()

	// Delete PVCs
	logger.Info("deleting pvcs")
	if err := perfTest.deletePVCs(ctx); err != nil {
		return tsStartTest, time.Now(), fmt.Errorf("failed to delete test pvcs: %w", err)
	} else {
		logger.Info("pvcs deleted")
	}
	logger.Info("wait for pvc deletion")
	if err := perfTest.waitForPVCsDeleted(ctx); err != nil {
		return tsStartTest, time.Now(), fmt.Errorf("failed to wait for deleted pvcs: %w", err)
	}
	logger.Info("pvc deletion verified")
	tsEndTest := time.Now()

	logger.Info("report",
		"start", tsStartTest,
		"pvc-created", tsPVCSCreated,
		"pvc-utilization", tsPVCSUsed,
		"pvc-free", tsPVCSFree,
		"pvc-deleted", tsEndTest,
		"end", tsEndTest)

	return tsStartTest, tsEndTest, nil
}

// createPVCs create the given number of pvcs
func (perfTest *PerfTest) createPVCs(ctx context.Context) error {
	for i := 0; i < perfTest.Instances; i++ {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", perfTest.NamePattern, i),
				Namespace: perfTest.TestNamespace,
				Labels:    perfTest.PVCLabels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: &perfTest.TestStorageClassName,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}
		if err := perfTest.Create(ctx, pvc); err != nil {
			return fmt.Errorf("could not create pvc %s (%v): %w", client.ObjectKeyFromObject(pvc), i, err)
		}
	}
	return nil
}

// createPods create the given number of pods
func (perfTest *PerfTest) createPods(ctx context.Context) error {
	for i := 0; i < perfTest.Instances; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", perfTest.NamePattern, i),
				Namespace: perfTest.TestNamespace,
				Labels:    perfTest.PVCLabels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "busybox",
						Image:   "public.ecr.aws/docker/library/busybox:1.36",
						Command: []string{"sh", "-c", "tail -f /dev/null"},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "data",
								MountPath: "/data"}},
					}},
				Volumes: []corev1.Volume{{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: fmt.Sprintf("%s-%d", perfTest.NamePattern, i),
						},
					}}},
			},
		}
		if err := perfTest.Create(ctx, pod); err != nil {
			return fmt.Errorf("could not create pod %s (%v): %w", client.ObjectKeyFromObject(pod), i, err)
		}
	}
	return nil
}

// deletePods delete the pods using the label
func (perfTest *PerfTest) deletePods(ctx context.Context) error {
	if err := perfTest.DeleteAllOf(ctx, &corev1.Pod{}, &client.DeleteAllOfOptions{
		ListOptions: client.ListOptions{
			LabelSelector: labels.SelectorFromSet(perfTest.PVCLabels),
			Namespace:     perfTest.TestNamespace,
		},
	}); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// waitForPodsRunning wait for pods to be in running state and return the count of running pods
func (perfTest *PerfTest) waitForPodsRunning(ctx context.Context) error {
	log.FromContext(ctx).Info("waiting until all pods are ready...")
	return wait.PollUntilContextCancel(ctx, 10*time.Second, true, func(ctx context.Context) (done bool, err error) {
		running := 0
		labelPods := labels.SelectorFromSet(perfTest.PVCLabels)
		podList := &corev1.PodList{}
		if err := perfTest.List(ctx, podList, &client.ListOptions{
			LabelSelector: labelPods,
			Namespace:     perfTest.TestNamespace,
		}); err != nil {
			return true, fmt.Errorf("could not get pod list: %w", err)
		}
		if len(podList.Items) == 0 {
			return true, fmt.Errorf("no pods found in %s by %s for run check", perfTest.TestNamespace, labelPods)
		}
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning {
				running++
			}
		}
		return running == len(podList.Items), nil
	})
}

// pvcsDeleted checks PVC's deleted or not
func (perfTest *PerfTest) waitForPVCsDeleted(ctx context.Context) error {
	return wait.PollUntilContextCancel(ctx, 10*time.Second, true, func(ctx context.Context) (done bool, err error) {
		pvcs := &corev1.PersistentVolumeClaimList{}
		if err := perfTest.List(ctx, pvcs, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(perfTest.PVCLabels),
			Namespace:     perfTest.TestNamespace,
		}); err != nil {
			return true, fmt.Errorf("could not check for PVCs: %w", err)
		}
		return len(pvcs.Items) == 0, nil
	})
}

// deletePVCs delete the PVC's using the label
func (perfTest *PerfTest) deletePVCs(ctx context.Context) error {
	if err := perfTest.DeleteAllOf(ctx, &corev1.PersistentVolumeClaim{}, &client.DeleteAllOfOptions{
		ListOptions: client.ListOptions{
			LabelSelector: labels.SelectorFromSet(perfTest.PVCLabels),
			Namespace:     perfTest.TestNamespace,
		},
	}); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// parseMetrics parse the values from a metrics result
func (unit *LvmUnitMetrics) parseMetrics(ctx context.Context, metricsResult string) (map[string]Metric, error) {
	/*` Metrics have the following structure:
	  {
	      "status": "success",
	      "data": {
	          "resultType": "matrix",
	          "result": [
	              {
	                  "metric": {
	                      "container": "kube-rbac-proxy"
	                  },
	                  "values": [
	                      [1654680100, "0.00010339026666666973"],
	                      [1654680106, "0.0005220373"],
	                      [1654680112, "0.0005220373"]
	                  ]
	              },
	              {
	                  "metric": {
	                      "container": "manager"
	                  },
	                  "values": [
	                      [1654680100, "0.00416508810000001"],
	                      [1654680106, "0.005354719799999981"],
	                      [1654680112, "0.005354719799999981"]
	                  ]
	              },
	          ]
	      }
	  }
	  `*/

	type subject struct {
		Pod       string
		Container string
	}
	type series struct {
		Metric subject
		Values [][]interface{}
	}
	type resultData struct {
		ResultType string
		Result     []series
	}
	type metricsData struct {
		Status string
		Data   resultData
	}

	var d metricsData
	err := json.Unmarshal([]byte(metricsResult), &d)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	var name string
	metrics := map[string]Metric{}

	for _, res := range d.Data.Result {
		var valuesList []float64
		for _, values := range res.Values {
			v, err := strconv.ParseFloat(fmt.Sprintf("%s", values[1]), 64)
			if err != nil {
				log.FromContext(ctx).Error(errors.New(fmt.Sprintf("error converting %s to number", values[1])), "parsing error")
			} else {
				valuesList = append(valuesList, v)
			}
		}

		if res.Metric.Pod != "" {
			name = res.Metric.Pod
		} else if res.Metric.Container != "" {
			name = res.Metric.Container
		} else {
			log.FromContext(ctx).Error(fmt.Errorf("subject not found"), "parsing error")
		}

		min, max, avg := getMinMaxAvg(valuesList)
		metrics[name] = Metric{
			Min: min,
			Max: max,
			Avg: avg,
		}
	}

	return metrics, nil
}

// getMetrics fetches the metrics from the server
func (unit *LvmUnitMetrics) getMetrics(ctx context.Context, start, end int64, query string) string {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	clnt := &http.Client{Transport: tr}

	params := url.Values{}
	params.Add("start", strconv.FormatInt(start, 10))
	params.Add("end", strconv.FormatInt(end, 10))
	params.Add("step", "6")
	params.Add("namespace", unit.Namespace)
	params.Add("query", query)
	params.Add("timeout", "30s")
	body := strings.NewReader("")

	metricsQuery := fmt.Sprintf("%s/api/v1/query_range?%s", unit.PrometheusURL, params.Encode())
	req, err := http.NewRequest(http.MethodGet, metricsQuery, body)

	if err != nil {
		log.FromContext(ctx).Error(err, "could not query prometheus")
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", unit.token()))

	resp, err := clnt.Do(req)
	if err != nil {
		log.FromContext(ctx).Error(err, "error in prometheus request")
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.FromContext(ctx).Error(fmt.Errorf("error during metrics readout: %s", resp.Status), "could not fetch metric")
		if resp.StatusCode == 403 {
			log.FromContext(ctx).Error(fmt.Errorf("Probably token expired, renew token and try to execute again providing the new token."), "could not fetch metric")
		}
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.FromContext(ctx).Error(err, "error reading metrics response", "url", req.URL)
	}

	return string(data)
}

// renderTemplate fills in the values of namespace, pod, workload etc in the template string
func (unit *LvmUnitMetrics) renderTemplate(metricsTemplate string) (string, error) {
	tmpl, err := template.New("test").Parse(metricsTemplate)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, unit)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// getPodMetrics gets the CPU and MEM metrics of pods
func (unit *LvmUnitMetrics) getPodMetrics(ctx context.Context, start, end int64) error {
	query, err := unit.renderTemplate(CpuQueryTemplatePod)
	if err != nil {
		return err
	}
	metricsRes := unit.getMetrics(ctx, start, end, query)
	if metricsRes == "" {
		return fmt.Errorf("CPU Metrics not found")
	}
	podCpu, err := unit.parseMetrics(ctx, metricsRes)
	if err != nil {
		return fmt.Errorf("CPU Metrics cannot be parsed: %w", err)
	}

	query, err = unit.renderTemplate(MemQueryTemplatePod)
	if err != nil {
		return err
	}
	metricsRes = unit.getMetrics(ctx, start, end, query)
	if metricsRes == "" {
		return fmt.Errorf("MEM Metrics not found")
	}
	podMem, err := unit.parseMetrics(ctx, metricsRes)
	if err != nil {
		return fmt.Errorf("MEM Metrics cannot be parsed: %w", err)
	}

	for key := range podCpu {
		unit.Metrics[key] = ObjectMetric{
			Name: key,
			Type: "Pod",
			Cpu: Metric{
				Min: podCpu[key].Min,
				Max: podCpu[key].Max,
				Avg: podCpu[key].Avg,
			},
			Mem: Metric{
				Min: podMem[key].Min / Mebibyte,
				Max: podMem[key].Max / Mebibyte,
				Avg: podMem[key].Avg / Mebibyte,
			},
		}
	}

	return nil
}

// getContainerMetrics gets the CPU and MEM metrics of containers
func (unit *LvmUnitMetrics) getContainerMetrics(ctx context.Context, start, end int64) error {
	query, err := unit.renderTemplate(CpuQueryTemplateContainer)
	if err != nil {
		return err
	}
	metricsRes := unit.getMetrics(ctx, start, end, query)
	if metricsRes == "" {
		return fmt.Errorf("CPU Metrics not found")
	}
	containerCpu, err := unit.parseMetrics(ctx, metricsRes)
	if err != nil {
		return fmt.Errorf("CPU Metrics cannot be parsed: %w", err)
	}

	query, err = unit.renderTemplate(MemQueryTemplateContainer)
	if err != nil {
		return err
	}
	metricsRes = unit.getMetrics(ctx, start, end, query)
	if metricsRes == "" {
		return fmt.Errorf("MEM Metrics not found")
	}
	containerMem, err := unit.parseMetrics(ctx, metricsRes)
	if err != nil {
		return fmt.Errorf("MEM Metrics cannot be parsed: %w", err)
	}

	for key := range containerCpu {
		unit.Metrics[key] = ObjectMetric{
			Name: key,
			Type: "Container",
			Cpu: Metric{
				Min: containerCpu[key].Min,
				Max: containerCpu[key].Max,
				Avg: containerCpu[key].Avg,
			},
			Mem: Metric{
				Min: containerMem[key].Min / Mebibyte,
				Max: containerMem[key].Max / Mebibyte,
				Avg: containerMem[key].Avg / Mebibyte,
			},
		}
	}
	return nil
}

// getAllMetrics gets the metrics of pods and containers
func (unit *LvmUnitMetrics) getAllMetrics(ctx context.Context, start, end time.Time, collector Collector) error {
	unit.Start = time.Unix(start.Unix(), 0)
	unit.End = time.Unix(end.Unix(), 0)

	unit.Metrics = make(map[string]ObjectMetric)
	log.FromContext(ctx).Info("getting pod metrics")
	if err := unit.getPodMetrics(ctx, start.Unix(), end.Unix()); err != nil {
		return fmt.Errorf("could not get pod metrics: %w", err)
	}
	for k, v := range unit.Metrics {
		collector.Metrics[k] = v
	}
	unit.printMetrics(ctx)

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot open working directory: %w", err)
	}

	if file, err := os.Create(filepath.Join(dir, fmt.Sprintf("pod-metrics-%s-%v-%v.toml", unit.Name, start.Unix(), end.Unix()))); err != nil {
		return fmt.Errorf("could not create metrics report file: %w", err)
	} else {
		if err := toml.NewEncoder(file).Encode(unit); err != nil {
			return fmt.Errorf("could not write metrics report file: %w", err)
		}
		log.FromContext(ctx).Info("pod metrics written", "file", file.Name())
	}

	unit.Metrics = make(map[string]ObjectMetric)
	log.FromContext(ctx).Info("getting container metrics")
	if err := unit.getContainerMetrics(ctx, start.Unix(), end.Unix()); err != nil {
		return fmt.Errorf("could not get container metrics: %w", err)
	}
	unit.printMetrics(ctx)
	if file, err := os.Create(filepath.Join(dir, fmt.Sprintf("container-metrics-%s-%v-%v.toml", unit.Name, start.Unix(), end.Unix()))); err != nil {
		return fmt.Errorf("could not create metrics report file: %w", err)
	} else {
		if err := toml.NewEncoder(file).Encode(unit); err != nil {
			return fmt.Errorf("could not write metrics report file: %w", err)
		}
		log.FromContext(ctx).Info("container metrics written", "file", file.Name())
	}
	return nil
}

// printMetrics prints the CPU and MEM metrics
func (unit *LvmUnitMetrics) printMetrics(ctx context.Context) {
	logger := log.FromContext(ctx)
	for _, u := range unit.Metrics {
		logger.Info("report",
			"workload-type", unit.WorkloadType,
			"workload-name", unit.WorkloadType,
			"type", u.Type,
			"name", u.Name,
			"start", unit.Start,
			"end", unit.End,
			"cpu-min", u.Cpu.Min,
			"cpu-max", u.Cpu.Max,
			"cpu-avg", u.Cpu.Avg,
			"mem-min", u.Mem.Min,
			"mem-max", u.Mem.Max,
			"mem-avg", u.Mem.Avg)
	}
}

// getMinMaxAvg returns min, max and avg elements from a slice
func getMinMaxAvg(array []float64) (float64, float64, float64) {

	if len(array) == 0 {
		return 0, 0, 0
	}

	var avg float64
	min := array[0]
	max := array[0]
	for _, value := range array {
		if min > value {
			min = value
		}
		if max < value {
			max = value
		}
		avg += value
	}
	return min, max, avg / float64(len(array))
}
