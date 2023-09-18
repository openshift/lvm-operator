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
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

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
	Start         time.Time
	End           time.Time
	Name          string
	Namespace     string
	WorkloadType  string
	ObjectMetrics map[string]ObjectMetric
}

const (
	CpuQueryTemplatePod       = `sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{cluster="", namespace="{{.Namespace}}"} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (pod)`
	CpuQueryTemplateContainer = `sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{cluster="", namespace="{{.Namespace}}"} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (container)`
	MemQueryTemplatePod       = `sum(container_memory_working_set_bytes{cluster="", namespace="{{.Namespace}}",container!="", image!=""} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (pod)`
	MemQueryTemplateContainer = `sum(container_memory_working_set_bytes{cluster="", namespace="{{.Namespace}}",container!="", image!=""} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (container)`
	Mebibyte                  = 1048576 // bytes
)

const (
	UsageHelp = `Usage of operf:
This script retrieves cpu and memory usage metrics for all the workloads created by the logical volume manager storage (lvms).

	It creates <instances> of "busybox" pods using PVCs provisioned via the Storage class provided
	Example:
	# go run test/performance/operf.go -token sha256~cj81ClyUYu7g05y8K-uLWm2AbrKTbNEQ96hEJcWStQo -sc lvms-vg1 -instances 16

Parameters:`
)

var (
	// flags
	Instances            *int
	TestNamespace        *string
	TestStorageClassName *string
	NamePattern          *string
	Token                *string

	// variables
	PrometheusURL string
	PVCLabels     = map[string]string{"app": "testPerformance"}
	Ctx           = context.Background()
)

func main() {
	Instances = flag.Int("instances", 4, "Number of Pods/Pvcs (each Pod uses a different PVC) to create in the test")
	TestNamespace = flag.String("namespace", "lvm-operator-system", "Namespace where operator is deployed and the PVCs and test pods will be deployed/undeployed")
	TestStorageClassName = flag.String("sc", "", "Name of the topolvm storage class that will be used in the PVCs")
	NamePattern = flag.String("pattern", "perfotest", "Pattern used to build the PVCs/Pods names")
	Token = flag.String("token", "", "Mandatory authentication token needed to connect with the Openshift cluster")

	flag.Usage = func() {
		writer := tabwriter.NewWriter(os.Stdout, 0, 8, 1, '\t', tabwriter.AlignRight)
		flagSet := flag.CommandLine
		fmt.Println(UsageHelp)
		order := []string{"instances", "namespace", "pattern", "sc", "token"}
		for _, name := range order {
			flag := flagSet.Lookup(name)
			fmt.Fprintf(writer, "\t-%s\t(default: %s)\t%s\n", flag.Name, flag.DefValue, flag.Usage)
		}
		writer.Flush()
	}

	flag.Parse()

	if *Token == "" {
		fmt.Println("Please provide the token parameter to connect with the Openshift cluster. Aborting test.")
		os.Exit(1)
	}
	if *TestStorageClassName == "" {
		fmt.Println("Please provide the sc parameter to use storage class during PVC creation. Aborting test.")
		os.Exit(1)
	}

	clientSet := getClient()

	PrometheusURL = getPrometheusURL(clientSet)
	if PrometheusURL == "" {
		panic("Not possible to collect metrics from Prometheus server. Aborting test.")
	}

	// Execute actions
	start, end := usageTest(clientSet)

	// Retrieve and print metrics
	units := []LvmUnitMetrics{
		{Namespace: *TestNamespace, Name: "lvms-operator", WorkloadType: "deployment"},
		{Namespace: *TestNamespace, Name: "topolvm-controller", WorkloadType: "deployment"},
		{Namespace: *TestNamespace, Name: "topolvm-node", WorkloadType: "daemonset"},
		{Namespace: *TestNamespace, Name: "vg-manager", WorkloadType: "daemonset"},
	}
	for _, unit := range units {
		unit.getAllMetrics(start, end)
	}
}

// getClient get the client to talk to the k8s server
func getClient() *kubernetes.Clientset {
	kubeconfig, ok := os.LookupEnv("KUBECONFIG")
	if !ok {
		panic("KUBECONFIG env var not set")
	}

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientSet
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	return clientSet
}

// usageTest run the create, delete tests
func usageTest(c *kubernetes.Clientset) (int64, int64) {
	tsStartTest := time.Now().Unix()
	// Create PVCs
	fmt.Printf("%d PVCs created\n", createPVC(c, *NamePattern, *Instances))
	tsPVCSCreated := time.Now().Unix()

	// Use PVCs
	fmt.Printf("%d Pods created\n", createPods(c, *NamePattern, *Instances))
	tsPVCSUsed := time.Now().Unix()

	// Wait for Pods Running
	fmt.Printf("%d Pods running\n", waitForPodsRunning(c))

	// Free PVCs
	if deletePods(c) != nil {
		fmt.Printf("Error trying to delete test Pods\n")
	} else {
		fmt.Printf("test Pods deleted\n")
	}
	tsPVCSFree := time.Now().Unix()

	// Delete PVCs
	if deletePVCs(c) != nil {
		fmt.Printf("Error trying to delete test PVCs\n")
	} else {
		fmt.Printf("test PVCs deleted\n")
	}
	tries := 0
	waitInterval := 10 * time.Second
	for !pvcsDeleted(c) {
		fmt.Printf("Waiting for PVCS clean \n")
		time.Sleep(waitInterval)
		tries++
		if tries == 20 {
			fmt.Printf("Timeout (%d seconds) waiting for the PVCs to be cleaned up", waitInterval)
			break
		}
	}
	tsEndTest := time.Now().Unix()

	fmt.Println("Times report")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("Start test      : %s\n", time.Unix(tsStartTest, 0))
	fmt.Printf("PVCs created    : %s\n", time.Unix(tsPVCSCreated, 0))
	fmt.Printf("PVCs utilization: %s\n", time.Unix(tsPVCSUsed, 0))
	fmt.Printf("PVCs Free       : %s\n", time.Unix(tsPVCSFree, 0))
	fmt.Printf("PVCs deleted    : %s\n", time.Unix(tsEndTest, 0))
	fmt.Printf("End test        : %s\n", time.Unix(tsEndTest, 0))

	fmt.Printf("Usage report: %d pods in namespace %s using %d PVCs with Storage Class <%s>\n",
		*Instances,
		*TestNamespace,
		*Instances,
		*TestStorageClassName)

	return tsStartTest, tsEndTest
}

// createPVC create the given number of pvcs
func createPVC(c *kubernetes.Clientset, pvcName string, instances int) int {
	var pvc = corev1.PersistentVolumeClaim{}
	var numPVCsCreated = 0

	for i := 0; i < instances; i++ {
		pvc = corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", pvcName, i),
				Namespace: *TestNamespace,
				Labels:    PVCLabels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: TestStorageClassName,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName(corev1.ResourceStorage): resource.MustParse("1Gi"),
					},
				},
			},
		}
		_, err := c.CoreV1().PersistentVolumeClaims(*TestNamespace).Create(Ctx, &pvc, metav1.CreateOptions{})
		if err != nil {
			fmt.Printf("Error creating PVC <%s-%d>: %s\n", pvcName, i, err)
		} else {
			numPVCsCreated++
		}
	}
	return numPVCsCreated
}

// createPods create the given number of pods
func createPods(c *kubernetes.Clientset, podName string, instances int) int {
	var pod = corev1.Pod{}
	var numPodsCreated = 0

	for i := 0; i < instances; i++ {
		pod = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", podName, i),
				Namespace: *TestNamespace,
				Labels:    PVCLabels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "busybox",
						Image:   "busybox",
						Command: []string{"tail", "-f", "/dev/null"},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "data",
								MountPath: "/data"}},
					}},
				Volumes: []corev1.Volume{{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: fmt.Sprintf("%s-%d", podName, i),
						},
					}}},
			},
		}
		_, err := c.CoreV1().Pods(*TestNamespace).Create(Ctx, &pod, metav1.CreateOptions{})
		if err != nil {
			fmt.Printf("Error creating Pod <%s-%d>: %s\n", podName, i, err)
		} else {
			numPodsCreated++
		}
	}
	return numPodsCreated
}

// deletePods delete the pods using the label
func deletePods(c *kubernetes.Clientset) error {
	labelPods := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPodOptions := metav1.ListOptions{LabelSelector: labelPods.String()}
	err := c.CoreV1().Pods(*TestNamespace).DeleteCollection(Ctx, metav1.DeleteOptions{}, listPodOptions)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// waitForPodsRunning wait for pods to be in running state and return the count of running pods
func waitForPodsRunning(c *kubernetes.Clientset) int {
	labelPods := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPodOptions := metav1.ListOptions{LabelSelector: labelPods.String()}
	podList, err := c.CoreV1().Pods(*TestNamespace).List(Ctx, listPodOptions)
	if err != nil {
		fmt.Printf("Error getting pod list: %s\n", err)
		return 0
	}
	if len(podList.Items) == 0 {
		fmt.Printf("No pods in %s with labels %s\n", *TestNamespace, labelPods)
		return 0
	}

	fmt.Printf("Waiting for pods running...\n")
	podsRunning := 0
	for _, pod := range podList.Items {
		isRunning := false
		for i := 0; i < 3; i++ {
			p, _ := c.CoreV1().Pods(*TestNamespace).Get(Ctx, pod.Name, metav1.GetOptions{})
			isRunning = p.Status.Phase == corev1.PodRunning
			fmt.Printf("Pod %s is in phase %s ...\n", p.Name, p.Status.Phase)
			if isRunning {
				podsRunning++
				break
			} else {
				time.Sleep(30 * time.Second)
			}
		}
	}
	return podsRunning
}

// pvcsDeleted checks PVC's deleted or not
func pvcsDeleted(c *kubernetes.Clientset) bool {
	labelPvc := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPVCOptions := metav1.ListOptions{LabelSelector: labelPvc.String()}
	pvcList, err := c.CoreV1().PersistentVolumeClaims(*TestNamespace).List(Ctx, listPVCOptions)
	if err != nil {
		fmt.Printf("Error getting pvcs list: %s\n", err)
		return false
	}
	return len(pvcList.Items) == 0
}

// deletePVCs delete the PVC's using the label
func deletePVCs(c *kubernetes.Clientset) error {
	labelPvc := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPvcOptions := metav1.ListOptions{LabelSelector: labelPvc.String()}
	err := c.CoreV1().PersistentVolumeClaims(*TestNamespace).DeleteCollection(Ctx, metav1.DeleteOptions{}, listPvcOptions)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// getPrometheusURL retrieve the prometheus url from a server
func getPrometheusURL(c *kubernetes.Clientset) string {
	data, err := c.RESTClient().
		Get().
		AbsPath("/apis/route.openshift.io/v1/namespaces/openshift-monitoring/routes/prometheus-k8s").
		DoRaw(Ctx)
	if err != nil {
		fmt.Println("Error getting Openshift Prometheus route:", err)
		return ""
	}
	var route map[string]interface{}
	err = json.Unmarshal([]byte(data), &route)
	if err != nil {
		panic("Failed to Unmarshal route")
	}
	host := route["spec"].(map[string]interface{})["host"]
	return fmt.Sprintf("https://%s", host)
}

// parseMetrics parse the values from a metrics result
func (unit *LvmUnitMetrics) parseMetrics(metricsResult string) map[string]Metric {
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
		panic("Failed to Unmarshal metrics")
	}

	var name string
	metrics := map[string]Metric{}

	for _, res := range d.Data.Result {
		valuesList := []float64{}
		for _, values := range res.Values {

			v, err := strconv.ParseFloat(fmt.Sprintf("%s", values[1]), 64)
			if err != nil {
				fmt.Println("error converting ", values[1], " to number")
			} else {
				valuesList = append(valuesList, v)
			}
		}

		if res.Metric.Pod != "" {
			name = res.Metric.Pod
		} else if res.Metric.Container != "" {
			name = res.Metric.Container
		} else {
			panic("subject not found")
		}

		min, max, avg := getMinMaxAvg(valuesList)
		metrics[name] = Metric{
			Min: min,
			Max: max,
			Avg: avg,
		}
	}

	return metrics
}

// getMetrics fetches the metrics from the server
func (unit *LvmUnitMetrics) getMetrics(start, end int64, query string) string {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	client := &http.Client{Transport: tr}

	params := url.Values{}
	params.Add("start", strconv.FormatInt(start, 10))
	params.Add("end", strconv.FormatInt(end, 10))
	params.Add("step", "6")
	params.Add("namespace", *TestNamespace)
	params.Add("query", query)
	params.Add("timeout", "30s")
	body := strings.NewReader("")

	metricsQuery := fmt.Sprintf("%s/api/v1/query_range?%s", PrometheusURL, params.Encode())
	req, err := http.NewRequest("GET", metricsQuery, body)

	if err != nil {
		fmt.Printf("Error querying Prometheus: %s", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", *Token))

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error in request: %s", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("Error in request:", resp.Status)
		if resp.StatusCode == 403 {
			fmt.Println("Probably token expired, renew token and try to execute again providing the new token.")
		}
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response from ", req.URL, ":", err)
	}

	return string(data)
}

// renderTemplate fills in the values of namespace, pod, workload etc in the template string
func (unit *LvmUnitMetrics) renderTemplate(metricsTemplate string) string {
	tmpl, err := template.New("test").Parse(metricsTemplate)
	if err != nil {
		panic(err)
	}
	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, unit)
	if err != nil {
		panic(err)
	}
	return buf.String()
}

// getPodMetrics gets the CPU and MEM metrics of pods
func (unit *LvmUnitMetrics) getPodMetrics(start, end int64) {
	query := unit.renderTemplate(CpuQueryTemplatePod)
	metricsRes := unit.getMetrics(start, end, query)
	if metricsRes == "" {
		panic("CPU Metrics not found")
	}
	podCpu := unit.parseMetrics(metricsRes)

	query = unit.renderTemplate(MemQueryTemplatePod)
	metricsRes = unit.getMetrics(start, end, query)
	if metricsRes == "" {
		panic("MEM Metrics not found")
	}
	podMem := unit.parseMetrics(metricsRes)

	for key := range podCpu {
		unit.ObjectMetrics[key] = ObjectMetric{
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
}

// getContainerMetrics gets the CPU and MEM metrics of containers
func (unit *LvmUnitMetrics) getContainerMetrics(start, end int64) {
	query := unit.renderTemplate(CpuQueryTemplateContainer)
	metricsRes := unit.getMetrics(start, end, query)
	if metricsRes == "" {
		panic("CPU Metrics not found")
	}
	containerCpu := unit.parseMetrics(metricsRes)

	query = unit.renderTemplate(MemQueryTemplateContainer)
	metricsRes = unit.getMetrics(start, end, query)
	if metricsRes == "" {
		panic("MEM Metrics not found")
	}
	containerMem := unit.parseMetrics(metricsRes)

	for key := range containerCpu {
		unit.ObjectMetrics[key] = ObjectMetric{
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
}

// getAllMetrics gets the metrics of pods and containers
func (unit *LvmUnitMetrics) getAllMetrics(start, end int64) {
	unit.Start = time.Unix(start, 0)
	unit.End = time.Unix(end, 0)

	unit.ObjectMetrics = map[string]ObjectMetric{}
	unit.getPodMetrics(start, end)
	unit.printMetrics()

	unit.ObjectMetrics = map[string]ObjectMetric{}
	unit.getContainerMetrics(start, end)
	unit.printMetrics()
}

// printMetrics prints the CPU and MEM metrics
func (unit *LvmUnitMetrics) printMetrics() {
	for _, u := range unit.ObjectMetrics {
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("WorkloadType: %s, WorkloadName: %s, Type: %s, Name: %s,  StartTime: %s, EndTime: %s", unit.WorkloadType, unit.Name, u.Type, u.Name, unit.Start, unit.End)
		fmt.Printf("\t CPU (min|max|avg) seconds: % 10.4f | % 10.4f | % 10.4f\n", u.Cpu.Min, u.Cpu.Max, u.Cpu.Avg)
		fmt.Printf("\t MEM (min|max|avg) Mib    : % 10.4f | % 10.4f | % 10.4f\n", u.Mem.Min, u.Mem.Max, u.Mem.Avg)
		fmt.Println(strings.Repeat("-", 80))
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
