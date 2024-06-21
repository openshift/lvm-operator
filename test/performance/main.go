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
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/zapr"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	configv1 "github.com/openshift/api/config/v1"
	routesv1 "github.com/openshift/api/route/v1"
	secv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	lvmv1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
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
	Instances                 int
	TestNamespace             string
	TestStorageClassName      string
	StorageClass              *v1.StorageClass
	NamePattern               string
	Token                     string
	OutDir                    string
	RunStress                 bool
	LongTermObservationWindow time.Duration

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

	if err := NewCmd(&PerfTest{}).Execute(); err != nil {
		os.Exit(1)
	}
}

// NewCmd creates a new CLI command
func NewCmd(perfTest *PerfTest) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "go run ./test/performance",
		Short: "Commands for running LVMS Performance Tests",
		Long: `This script retrieves cpu and memory usage metrics for all the workloads created by the logical volume manager storage (lvms).

It creates <instances> of "busybox" pods using PVCs provisioned via the Storage class provided, when in stress test mode, or only collects metrics in idle.

A report is written as "metrics-<start_unix_ts>-<end_unix_ts>.toml" in <output-directory> or current working directory if not set.

Quantile Units in report:
CPU: mcores
Memory: MiB
`,
		SilenceErrors: false,
		Example: `Stress: go run ./test/performance -t $(oc whoami -t) -s lvms-vg1 -i 64
Idle: go run ./test/performance -t $(oc whoami -t) --run-stress false --long-term-observation-window=5m`,
		SilenceUsage: true,
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

	cmd.Flags().IntVarP(&perfTest.Instances, "instances", "i", 4, "Number of Workloads/Pvcs (each Workload uses a different PVC) to create in the test")
	cmd.Flags().StringVarP(&perfTest.TestNamespace, "namespace", "n", "openshift-storage", "Namespace where operator is deployed and the PVCs and test pods will be deployed/undeployed")
	cmd.Flags().StringVarP(&perfTest.TestStorageClassName, "storage-class", "s", "lvms-vg1", "Name of the topolvm storage class that will be used in the PVCs")
	cmd.Flags().StringVarP(&perfTest.NamePattern, "pattern", "p", "operf", "Pattern used to build the PVCs/Workloads names")
	cmd.Flags().StringVarP(&perfTest.Token, "token", "t", "", "authentication token needed to connect with the Openshift cluster.")
	cmd.Flags().StringVarP(&perfTest.OutDir, "output-directory", "o", "", "output directory for the metrics report, working directory by default")

	cmd.Flags().BoolVarP(&perfTest.RunStress, "run-stress", "r", true, "defines if stress tests should be used, if false uses long-term observation and does not attempt to create stress resources")
	cmd.Flags().DurationVarP(&perfTest.LongTermObservationWindow, "long-term-observation-window", "w", 5*time.Minute, "only used when not running stress test, defines observation windows as duration into the past, e.g. 5m means the last 5 minutes")
	return cmd
}

func run(ctx context.Context, perfTest *PerfTest) error {
	if perfTest.Token == "" {
		return fmt.Errorf("authentication token for metrics extraction out of prometheus was not supplied")
	}

	var start, end time.Time
	if perfTest.RunStress {
		var err error
		if start, end, err = perfTest.run(ctx); err != nil {
			return fmt.Errorf("usage tests encountered an error (start %s, end %s): %w",
				start, end, err)
		}
	} else {
		start, end = time.Now().Add(-perfTest.LongTermObservationWindow), time.Now()
	}

	collector := NewCollector(
		perfTest.PrometheusURL,
		perfTest.Token,
		perfTest.TestNamespace,
		start, end,
	)

	// now we filter out all pods that contain the test pattern
	collector.SetPodFilter(perfTest.NamePattern)

	nodes := &corev1.NodeList{}
	if err := perfTest.List(ctx, nodes, client.HasLabels{"capacity.topolvm.io/00default"}); err != nil {
		log.FromContext(ctx).Error(fmt.Errorf("cannot get node list for avg-per-node: %w", err), "failed to calculate average per node")
	} else {
		collector.SetLVMNodes(len(nodes.Items))
	}

	if err := collector.collect(ctx); err != nil {
		return fmt.Errorf("could not collect metrics: %w", err)
	}

	report, err := collector.serialize(perfTest.OutDir)
	if err != nil {
		return fmt.Errorf("failed to serialize metrics: %w", err)
	}
	log.FromContext(ctx).Info("report written", "file", report.Name())

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
	logger.Info("pvcs created")
	tsPVCSCreated := time.Now()

	logger.Info("creating pods")
	if err := perfTest.createPods(ctx); err != nil {
		return tsStartTest, time.Now(), err
	}
	logger.Info("pods created")
	tsPVCSUsed := time.Now()

	if err := perfTest.waitForPodsRunning(ctx); err != nil {
		return tsStartTest, time.Now(), fmt.Errorf("failed to wait for running pods: %w", err)
	}
	logger.Info("all pods are running")

	logger.Info("deleting pods")
	if err := perfTest.deletePods(ctx); err != nil {
		return tsStartTest, time.Now(), fmt.Errorf("failed to delete test pods: %w", err)
	} else {
		logger.Info("all pods deleted")
	}
	tsPVCSFree := time.Now()

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
				Resources: corev1.VolumeResourceRequirements{
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
