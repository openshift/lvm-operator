/*
Copyright 2021.
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
	"io/ioutil"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type compMetrics struct {
	component string
	peakData []int64 // cpu, memory, filesystem
	avgData []int64  // cpu, memory, filesystem
}

type metric struct {
	Max float64
	Avg float64
}

type lvmUnitMetrics struct {
	Namespace string
	Name string
	WorkloadType string
	cpu metric
	ram metric
	fs metric
	start time.Time
	end time.Time
}

type topic struct {
	topic string
	queryTemplate string
	max float64
	avg float64
}

const (
	cpuQueryTemplate = `sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{cluster="", namespace="{{.Namespace}}"} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (pod)`
	memQueryTemplate = `sum(container_memory_working_set_bytes{cluster="", namespace="{{.Namespace}}",container!="", image!=""} * on(namespace,pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="{{.Namespace}}", workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (pod)`
	filesystemTemplate = `sum(pod:container_fs_usage_bytes:sum * on(pod) group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{workload="{{.Name}}", workload_type="{{.WorkloadType}}"}) by (pod)`
	Mebibyte = 1048576 // bytes
)

var (
	testNamespace *string
	testStorageClassName *string
	lvmClusterFile *string
	namePattern *string
	numberInstances *int
	token *string
	PVCLabels = map[string]string{"app": "testPerformance"}
	prometheusURL string
	results []compMetrics
)

func main() {
	testNamespace = flag.String("namespace", "lvm-operator-system", "Namespace where the PVCs and test pods will be deployed/undeployed")
	namePattern = flag.String("pattern", "perfotest", "Pattern used to build the PVCs/Pods names")
	numberInstances = flag.Int("number", 4, "Number of Pods/Pvcs (each Pod uses a different PVC) to create in the test")
	testStorageClassName = flag.String("sc", "topolvm-test", "Name of the topolvm storage class that will be used in the PVCs")
	lvmClusterFile = flag.String("lvmcrd", "", "CRD file for the lvm cluster. Using this parameter will only execute the <LVMcluster creation> (VGs and SC creation)")
	token = flag.String("token", "", "Mandatory authentication token needed to connect with the Openshift cluster\n\t# oc login -u <username>\n\t(...)\n\t# oc whoami -t\n\t(.. will yield <TOKEN> ..)")

	flag.Usage = func() {
		flagSet := flag.CommandLine
		fmt.Println(
`Usage of operf:
This script provide  LVM operator metrics retrieval (for all the units in the operator) doing two different kind of tests:

- LVMcluster creation: 
    This implies deployment of daemonsets in the nodes selected and the LVM Volume Groups creation.
	Example:
       # go run operf.go -token sha256~cj81ClyUYu7g05y8K-uLWm2AbrKTbNEQ96hEJcWStQo -lvmcrd ../../lvmcluster.yaml
		
- PVCs and PVs creation and usage:
	It is created <number> of "busy" pods using PVCs bounded to PVs which use the Storage class provided
	Example:
       # go run operf.go -token sha256~cj81ClyUYu7g05y8K-uLWm2AbrKTbNEQ96hEJcWStQo -number 16

Parameters:
`)
		order := []string{"namespace", "pattern", "number", "sc", "lvmcrd", "token"}
		for _, name := range order {
			flag := flagSet.Lookup(name)
			fmt.Printf("-%s (default: %s)\t%s\n", flag.Name, flag.DefValue, flag.Usage)
		}
	}

	flag.Parse()
	var kubeconfig *string

	if *token == "" {
		fmt.Println("Please provide the token parameter to connect with the Openshift cluster. Aborting test.")
		return
	}

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientSet
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	prometheusURL = getPrometheusURL(clientSet)
	if prometheusURL == "" {
		fmt.Println("Not possible to collect metrics from Prometheus server. Aborting test.")
		return
	}

	// Execute actions
	var start int64
	var end int64
	if *lvmClusterFile != "" {
		start, end = creationTest(clientSet)
	} else {
		start, end = usageTest(clientSet)
	}
	//Retrieve and print metrics
	units := []lvmUnitMetrics{lvmUnitMetrics{Namespace: *testNamespace, Name: "vg-manager", WorkloadType: "daemonset"},
		                      lvmUnitMetrics{Namespace: *testNamespace, Name: "topolvm-node", WorkloadType: "daemonset"},
		                      lvmUnitMetrics{Namespace: *testNamespace, Name: "controller-manager", WorkloadType: "deployment"},
		                      lvmUnitMetrics{Namespace: *testNamespace, Name: "topolvm-controller", WorkloadType: "deployment"}}
	for _, unit := range units {
		unit.getAllMetrics(start, end)
		unit.printMetrics(false)
	}
	return
}

func creationTest(c *kubernetes.Clientset) (int64, int64) {
	tsStartTest := time.Now().Unix()
	cmd := exec.Command("oc", "apply", "-f", *lvmClusterFile)
	stdout, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error creating lvmCluster resource from <%s>: %s\n", *lvmClusterFile, err)
		return time.Now().Unix(), time.Now().Unix()
	} else {
		fmt.Println(string(stdout))
	}

	// Wait to have workloads
	found := false
	topolvmNodeFound := false
	vgManagerFound := false
	controllerManagerFound := false
	topolvmControllerFound := false
	attempts := 0
	for waitDeployed := true; waitDeployed; waitDeployed = !found {
		daemonsets, err := c.AppsV1().DaemonSets(*testNamespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Println("Error listing daemonsets. Aborting test")
			return 0, 0
		}
		deployments, err := c.AppsV1().Deployments(*testNamespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Println("Error listing deployments. Aborting test")
			return time.Now().Unix(), time.Now().Unix()
		}
		for _, ds := range daemonsets.Items {
			if ds.Name == "topolvm-node" {
				topolvmNodeFound = true
			}
			if ds.Name == "vg-manager" {
				vgManagerFound = true
			}
		}
		for _, ds := range deployments.Items {
			if ds.Name == "controller-manager" {
				controllerManagerFound = true
			}
			if ds.Name == "topolvm-controller" {
				topolvmControllerFound = true
			}
		}
		found = topolvmNodeFound && vgManagerFound && controllerManagerFound && topolvmControllerFound
		if attempts == 20 && !found {
			fmt.Println("Giving up waiting for workloads creation. Aborting test")
			return time.Now().Unix(), time.Now().Unix()
		} else {
			fmt.Println("Waiting for workloads creation.")
			attempts++
		}
		time.Sleep(5 * time.Second)
	}

	//check all in place
	allRunning := false
	attempts = 0
	for waiting := true; waiting; waiting = !allRunning {
		controllerManager, _ := c.AppsV1().Deployments(*testNamespace).Get(context.TODO(), "controller-manager", metav1.GetOptions{})
		topolvmController, _ := c.AppsV1().Deployments(*testNamespace).Get(context.TODO(), "topolvm-controller", metav1.GetOptions{})
		vgManager, _ := c.AppsV1().DaemonSets(*testNamespace).Get(context.TODO(), "vg-manager", metav1.GetOptions{})
		topolvmNode, _ := c.AppsV1().DaemonSets(*testNamespace).Get(context.TODO(), "topolvm-node", metav1.GetOptions{})

		allRunning = controllerManager.Status.Replicas == controllerManager.Status.AvailableReplicas &&
			         topolvmController.Status.Replicas == topolvmController.Status.AvailableReplicas &&
			         vgManager.Status.CurrentNumberScheduled == vgManager.Status.DesiredNumberScheduled &&
			         topolvmNode.Status.CurrentNumberScheduled == topolvmNode.Status.DesiredNumberScheduled
		if !allRunning {
			fmt.Println("Waiting for pods running")
			time.Sleep(10 * time.Second)
			attempts++
			if attempts == 20 {
				fmt.Println("Giving up waiting for operator pods running. Aborting test")
				return time.Now().Unix(), time.Now().Unix()
			}
		}
	}
	tsEndTest :=  time.Now().Unix()

	fmt.Println("Times report")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("Start test      : %s\n", time.Unix(tsStartTest, 0))
	fmt.Printf("End test        : %s\n", time.Unix(tsEndTest, 0))

	return tsStartTest, tsEndTest
}
func usageTest (c *kubernetes.Clientset) (int64, int64) {
	tsStartTest := time.Now().Unix()
	// Create PVCs
	fmt.Printf("%d PVCs created\n", createPVC(c, *namePattern, *numberInstances))
	tsPVCSCreated := time.Now().Unix()

	// Use PVCs
	fmt.Printf("%d Pods created\n", createPods(c, *namePattern, *numberInstances))
	tsPVCSUsed := time.Now().Unix()

	//Wait for Pods Running
	fmt.Printf("%d Pods running\n", waitForPodsRunning(c))

	// Free PVCs
	if deletePods(c) != nil {
		fmt.Printf("Error trying to delete test Pods: %s\n")
	} else {
		fmt.Printf("test Pods deleted\n")
	}
	tsPVCSFree := time.Now().Unix()

	// Delete PVCs
	if deletePVCs(c) != nil {
		fmt.Printf("Error trying to delete test PVCs: %s\n")
	} else {
		fmt.Printf("test PVCs deleted\n")
	}
	tries:=0
	waitInterval := 10 * time.Second
	for notDeleted := true; notDeleted; notDeleted = !pvcsDeleted(c) {
		fmt.Printf("Waiting for PVCS clean \n")
		time.Sleep(waitInterval)
		tries++
		if tries==20 {
			fmt.Printf("Timeout (%d seconds)waiting for PVCS cleaning", waitInterval)
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
		*numberInstances,
		*testNamespace,
		*numberInstances,
		*testStorageClassName)

	return tsStartTest, tsEndTest
}

func createPVC(c *kubernetes.Clientset, PVCName string, PVCsNumber int) int {
	var pvc = v1.PersistentVolumeClaim{}
	var numPVCsCreated = 0

	for i := 0; i < PVCsNumber; i++ {
		pvc = v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", PVCName, i),
				Namespace: *testNamespace,
				Labels:    PVCLabels,
			},
			Spec: v1.PersistentVolumeClaimSpec{
				AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				StorageClassName: testStorageClassName,
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(v1.ResourceStorage): resource.MustParse("128Mi"),
					},
				},
			},
		}
		_, err := c.CoreV1().PersistentVolumeClaims(*testNamespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
		if err != nil {
			fmt.Printf("Error creating PVC <%s-%d>: %s\n", PVCName, i, err)
		} else {
			numPVCsCreated++
		}
	}
	return numPVCsCreated
}

func createPods(c *kubernetes.Clientset, PodName string, PVCsNumber int) int {
	var pod = v1.Pod{}
	var numPodsCreated = 0

	for i := 0; i < PVCsNumber; i++ {
		pod = v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", PodName, i),
				Namespace: *testNamespace,
				Labels:    PVCLabels,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					v1.Container{
						Name: "busybox",
						Image: "busybox",
						Command: []string{"tail", "-f", "/dev/null"},
						VolumeMounts: []v1.VolumeMount{
							v1.VolumeMount{
								Name: "data",
								MountPath: "/data"}},
				}},
				Volumes: []v1.Volume{v1.Volume{
										Name: "data",
										VolumeSource: v1.VolumeSource{
												PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
												ClaimName: fmt.Sprintf("%s-%d", PodName, i),
											},
										}}},
			},
		}
		_, err := c.CoreV1().Pods(*testNamespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
		if err != nil {
			fmt.Printf("Error creating Pod <%s-%d>: %s\n", PodName, i, err)
		} else {
			numPodsCreated++
		}
	}
	return numPodsCreated
}

func deletePods(c *kubernetes.Clientset) error {
	labelPods := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPodOptions := metav1.ListOptions{LabelSelector: labelPods.String()}
	err := c.CoreV1().Pods(*testNamespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, listPodOptions)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func waitForPodsRunning(c *kubernetes.Clientset) int {
	labelPods := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPodOptions := metav1.ListOptions{LabelSelector: labelPods.String()}
	podList, err := c.CoreV1().Pods(*testNamespace).List(context.TODO(), listPodOptions)
	if err != nil {
		fmt.Printf("Error getting pod list: %s\n", err)
		return 0
	}
	if len(podList.Items) == 0 {
		fmt.Printf("No pods in %s with labels %s\n", *testNamespace, labelPods)
		return 0
	}

	fmt.Printf("Waiting for pods running...\n")
	podsRunning := 0
	for _, pod := range podList.Items {
		isRunning := false
		for i := 0; i < 3; i++ {
			p, _ := c.CoreV1().Pods(*testNamespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			isRunning = p.Status.Phase == v1.PodRunning
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

func pvcsDeleted(c *kubernetes.Clientset) bool {
	labelPvc := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPVCOptions := metav1.ListOptions{LabelSelector: labelPvc.String()}
	PVCList, err := c.CoreV1().PersistentVolumeClaims(*testNamespace).List(context.TODO(), listPVCOptions)
	if err != nil {
		fmt.Printf("Error getting pvcs list: %s\n", err)
		return false
	}
	return len(PVCList.Items) == 0
}

func deletePVCs(c *kubernetes.Clientset) error {
	labelPvc := labels.SelectorFromSet(labels.Set(PVCLabels))
	listPvcOptions := metav1.ListOptions{LabelSelector: labelPvc.String()}
	err := c.CoreV1().PersistentVolumeClaims(*testNamespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, listPvcOptions)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func getPrometheusURL(c *kubernetes.Clientset) string {
	data, err := c.RESTClient().
		Get().
		AbsPath("/apis/route.openshift.io/v1/namespaces/openshift-monitoring/routes/prometheus-k8s").
		DoRaw(context.TODO())
	if err != nil {
		fmt.Println("Error getting Openshift Prometheus route:", err)
		return ""
	}
	var route map[string]interface{}
	json.Unmarshal([]byte(data), &route)
	host := route["spec"].(map[string]interface{})["host"]
	return fmt.Sprintf("https://%s", host)
}

func (unit lvmUnitMetrics) getValues(metrics string) (float64, float64) {
	/* Metrics have the following structure:
	{"status":"success",
	 "data":{"resultType":"matrix",
	         "result":[
	                    {"metric":{"pod":"topolvm-controller-58c8f86445-gxtb4"},
	                     "values":[[1641983701,"164327424"],[1641983707,"164327424"],...]
	                    }
	                   ]
	        }
	}
	*/
	type subject struct {
		Pod string
	}
	type series struct {
		Metric subject
		Values [][]interface{}
	}
	type resultData struct {
		ResultType string
		Result []series
	}
	type metricsData struct {
		Status string
		Data resultData
	}
	var d metricsData
	var max float64 = 0
	var avg float64 = 0
	var n float64 = 0
	json.Unmarshal([]byte(metrics), &d)
	values := d.Data.Result[0].Values
	for i:=0; i < len(values); i++ {
		v, err := strconv.ParseFloat(fmt.Sprintf("%s", values[i][1]), 64)
		if err != nil {
			fmt.Println("error converting ", values[i][1], " to number")
		} else {
			if v > max {
				max = v
			}
			avg += v
			n++
		}
	}
	if n > 0 {
		avg = avg / n
	} else {
		avg = 0
	}
	return max, avg
}

func (unit lvmUnitMetrics) getMetrics(start int64, end int64, query string) string {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	params := url.Values{}
	params.Add("start", strconv.FormatInt(start, 10))
	params.Add("end", strconv.FormatInt(end, 10))
	params.Add("step", "6")
	params.Add("namespace", "lvm-operator-system")
	params.Add("query", query)
	params.Add("timeout", "30s")
	body := strings.NewReader("")

	metricsQuery := fmt.Sprintf("%s/api/v1/query_range?%s", prometheusURL, params.Encode())
	req, err := http.NewRequest("GET", metricsQuery, body)

	if err != nil {
		fmt.Printf("Error querying Prometheus: %s", err)
	}
	req.Header.Set("Authorization",  fmt.Sprintf("Bearer %s", *token))

	resp, err := client.Do(req)
	defer resp.Body.Close()

	if err != nil {
		fmt.Printf("Error in request: %s", err)
		return ""
	}
	if resp.StatusCode != 200 {
		fmt.Println("Error in request:", resp.Status)
		if resp.StatusCode == 403 {
			fmt.Println("Probably token expired, renew token and try to execute again providing the new token.")
		}
		return ""
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err !=nil {
		fmt.Println("Error reading response from ", req.URL, ":", err)
	}

	return string(data)

}

func (unit *lvmUnitMetrics) renderTemplate(metricsTemplate string) string {
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

func (unit *lvmUnitMetrics) getCPUMetrics(start int64, end int64) {
	query := unit.renderTemplate(cpuQueryTemplate)
	metrics := unit.getMetrics(start, end, query)
	unit.cpu.Max, unit.cpu.Avg = unit.getValues(metrics)
}

func (unit *lvmUnitMetrics) getRAMMetrics(start int64, end int64) {
	query := unit.renderTemplate(memQueryTemplate)
	metrics := unit.getMetrics(start, end, query)
	max, avg := unit.getValues(metrics)
	unit.ram.Max= max/Mebibyte
	unit.ram.Avg = avg/Mebibyte
}

func (unit *lvmUnitMetrics) getFSMetrics(start int64, end int64) {
	query := unit.renderTemplate(filesystemTemplate)
	metrics := unit.getMetrics(start, end, query)
	max, avg := unit.getValues(metrics)
	unit.fs.Max = max/Mebibyte
	unit.fs.Avg = avg/Mebibyte
}

func (unit *lvmUnitMetrics) getAllMetrics(start int64, end int64) {
	unit.start = time.Unix(start, 0)
	unit.end = time.Unix(end, 0)
	unit.getCPUMetrics(start, end)
	unit.getRAMMetrics(start, end)
	unit.getFSMetrics(start, end)

}

func (unit *lvmUnitMetrics) printMetrics(header bool) {
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("Report for %s %s between %s and %s\n", unit.Name, unit.WorkloadType, unit.start, unit.end)
	fmt.Printf("      CPU (max|avg) seconds: % 10.4f |  % 10.4f\n", unit.cpu.Max, unit.cpu.Avg)
	fmt.Printf("      RAM (max|avg) Mib    : % 10.4f |  % 10.4f\n", unit.ram.Max, unit.ram.Avg)
	fmt.Printf("      FS  (max|avg) Mib    : % 10.4f |  % 10.4f\n", unit.fs.Max, unit.fs.Avg)
	fmt.Println(strings.Repeat("-", 80))
}