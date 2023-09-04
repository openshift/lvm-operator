/*
Copyright © 2023 Red Hat, Inc.

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
	"errors"
	"flag"
	"fmt"
	"os"

	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/lvm-operator/controllers/node"
	"k8s.io/client-go/discovery"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	"github.com/openshift/library-go/pkg/config/leaderelection"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/controllers"
	persistent_volume "github.com/openshift/lvm-operator/controllers/persistent-volume"
	persistent_volume_claim "github.com/openshift/lvm-operator/controllers/persistent-volume-claim"
	"github.com/openshift/lvm-operator/pkg/cluster"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	//+kubebuilder:scaffold:imports
)

var (
	scheme                  = runtime.NewScheme()
	setupLog                = ctrl.Log.WithName("setup")
	operatorNamespaceEnvVar = "POD_NAMESPACE"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(lvmv1alpha1.AddToScheme(scheme))
	utilruntime.Must(topolvmv1.AddToScheme(scheme))
	utilruntime.Must(snapapi.AddToScheme(scheme))
	utilruntime.Must(secv1.Install(scheme))
	utilruntime.Must(configv1.Install(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logr := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logr)
	klog.SetLogger(logr)

	operatorNamespace, err := getOperatorNamespace()
	if err != nil {
		setupLog.Error(err, "unable to get operatorNamespace"+
			"Exiting")
		os.Exit(1)
	}
	setupLog.Info("Watching namespace", "Namespace", operatorNamespace)

	setupClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to initialize setup client for pre-manager startup checks")
		os.Exit(1)
	}
	snoCheck := cluster.NewMasterSNOCheck(setupClient)
	leaderElectionResolver, err := cluster.NewLeaderElectionResolver(snoCheck, enableLeaderElection, operatorNamespace)
	if err != nil {
		setupLog.Error(err, "unable to setup leader election")
		os.Exit(1)
	}
	leaderElectionConfig, err := leaderElectionResolver.Resolve(context.Background())
	if err != nil {
		setupLog.Error(err, "unable to resolve leader election config")
		os.Exit(1)
	}
	le, err := leaderelection.ToLeaderElectionWithLease(
		ctrl.GetConfigOrDie(), leaderElectionConfig, "lvms", "")
	if err != nil {
		setupLog.Error(err, "unable to setup leader election with lease configuration")
		os.Exit(1)
	}

	clusterType, err := cluster.NewTypeResolver(setupClient).GetType(context.Background())
	if err != nil {
		setupLog.Error(err, "unable to determine cluster type")
		os.Exit(1)
	}

	enableSnapshotting := true
	vsc := &snapapi.VolumeSnapshotClassList{}
	if err := setupClient.List(context.Background(), vsc, &client.ListOptions{Limit: 1}); err != nil {
		// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
		if discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
			setupLog.Info("VolumeSnapshotClasses do not exist on the cluster, ignoring")
			enableSnapshotting = false
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                              scheme,
		MetricsBindAddress:                  metricsAddr,
		Port:                                9443,
		HealthProbeBindAddress:              probeAddr,
		LeaderElectionResourceLockInterface: le.Lock,
		LeaderElection:                      !leaderElectionConfig.Disable,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// register controllers
	if err = (&controllers.LVMClusterReconciler{
		Client:                           mgr.GetClient(),
		Scheme:                           mgr.GetScheme(),
		EventRecorder:                    mgr.GetEventRecorderFor("LVMClusterReconciler"),
		ClusterType:                      clusterType,
		Namespace:                        operatorNamespace,
		TopoLVMLeaderElectionPassthrough: leaderElectionConfig,
		EnableSnapshotting:               enableSnapshotting,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LVMCluster")
		os.Exit(1)
	}

	if !snoCheck.IsSNO(context.Background()) {
		setupLog.Info("starting node-removal controller to observe node removal in MultiNode")
		if err = (&node.RemovalController{
			Client: mgr.GetClient(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "NodeRemovalControlelr")
			os.Exit(1)
		}
	}

	if err = mgr.GetFieldIndexer().IndexField(context.Background(), &lvmv1alpha1.LVMVolumeGroupNodeStatus{}, "metadata.name", func(object client.Object) []string {
		return []string{object.GetName()}
	}); err != nil {
		setupLog.Error(err, "unable to create name index on LVMVolumeGroupNodeStatus")
		os.Exit(1)
	}

	if err = (&lvmv1alpha1.LVMCluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "LVMCluster")
		os.Exit(1)
	}

	pvController := persistent_volume.NewPersistentVolumeReconciler(mgr.GetClient(), mgr.GetAPIReader(), mgr.GetEventRecorderFor("lvms-pv-controller"))
	if err := pvController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PersistentVolume")
		os.Exit(1)
	}

	pvcController := persistent_volume_claim.NewPersistentVolumeClaimReconciler(mgr.GetClient(), mgr.GetEventRecorderFor("lvms-pvc-controller"))
	if err := pvcController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PersistentVolumeClaim")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// getOperatorNamespace returns the Namespace the operator should be watching for changes
func getOperatorNamespace() (string, error) {
	// The env variable POD_NAMESPACE which specifies the Namespace the pod is running in
	// and hence will watch.

	ns, found := os.LookupEnv(operatorNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s not found", operatorNamespaceEnvVar)
	}
	return ns, nil
}
