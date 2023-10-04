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

package operator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/openshift/lvm-operator/internal/controllers/lvmcluster"
	"github.com/openshift/lvm-operator/internal/controllers/node"
	"github.com/openshift/lvm-operator/internal/controllers/persistent-volume"
	"github.com/openshift/lvm-operator/internal/controllers/persistent-volume-claim"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	"github.com/openshift/library-go/pkg/config/leaderelection"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/cluster"
	//+kubebuilder:scaffold:imports
)

const (
	DefaultDiagnosticsAddr      = ":8443"
	DefaultProbeAddr            = ":8081"
	DefaultEnableLeaderElection = false
)

var DefaultVGManagerCommand = []string{"/lvms", "vgmanager"}

type Options struct {
	Scheme   *runtime.Scheme
	SetupLog logr.Logger

	diagnosticsAddr      string
	healthProbeAddr      string
	enableLeaderElection bool

	vgManagerCommand []string
}

// NewCmd creates a new CLI command
func NewCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "operator",
		Short:         "Commands for running LVMS Operator",
		Long:          `Operator reconciling LVMCluster LVMVolumeGroup and LVMVolumeGroupNodeStatus`,
		SilenceErrors: false,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, args, opts)
		},
	}

	cmd.Flags().StringVar(
		&opts.diagnosticsAddr, "diagnostics-address", DefaultDiagnosticsAddr, "The address the diagnostics endpoint binds to.",
	)
	cmd.Flags().StringVar(
		&opts.healthProbeAddr, "health-probe-bind-address", DefaultProbeAddr, "The address the probe endpoint binds to.",
	)
	cmd.Flags().BoolVar(
		&opts.enableLeaderElection, "leader-elect", DefaultEnableLeaderElection,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.",
	)

	cmd.Flags().StringSliceVar(
		&opts.vgManagerCommand, "vgmanager-cmd", DefaultVGManagerCommand, "The command that should be used to start vgmanager on the node. Useful for debugging purposes but normally not changed.",
	)

	return cmd
}

func run(cmd *cobra.Command, _ []string, opts *Options) error {
	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("unable to get operatorNamespace: %w", err)
	}

	opts.SetupLog.Info("Watching namespace", "Namespace", operatorNamespace)

	setupClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: opts.Scheme})
	if err != nil {
		return fmt.Errorf("unable to initialize setup client for pre-manager startup checks: %w", err)
	}

	snoCheck := cluster.NewMasterSNOCheck(setupClient)
	leaderElectionResolver, err := cluster.NewLeaderElectionResolver(snoCheck, opts.enableLeaderElection, operatorNamespace)
	if err != nil {
		return fmt.Errorf("unable to setup leader election: %w", err)
	}

	leaderElectionConfig, err := leaderElectionResolver.Resolve(cmd.Context())
	if err != nil {
		return fmt.Errorf("unable to resolve leader election config: %w", err)
	}

	le, err := leaderelection.ToLeaderElectionWithLease(
		ctrl.GetConfigOrDie(), leaderElectionConfig, "lvms", "")
	if err != nil {
		return fmt.Errorf("unable to setup leader election with lease configuration: %w", err)
	}

	clusterType, err := cluster.NewTypeResolver(setupClient).GetType(context.Background())
	if err != nil {
		return fmt.Errorf("unable to determine cluster type: %w", err)
	}

	enableSnapshotting := true
	vsc := &snapapi.VolumeSnapshotClassList{}
	if err := setupClient.List(context.Background(), vsc, &client.ListOptions{Limit: 1}); err != nil {
		// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
		if meta.IsNoMatchError(err) {
			opts.SetupLog.Info("VolumeSnapshotClasses do not exist on the cluster, ignoring")
			enableSnapshotting = false
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: opts.Scheme,
		Metrics: metricsserver.Options{
			BindAddress:    opts.diagnosticsAddr,
			SecureServing:  true,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
		},
		WebhookServer: &webhook.DefaultServer{Options: webhook.Options{
			Port: 9443,
		}},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{operatorNamespace: {}},
		},
		HealthProbeBindAddress:              opts.healthProbeAddr,
		LeaderElectionResourceLockInterface: le.Lock,
		LeaderElection:                      !leaderElectionConfig.Disable,
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// register controllers
	if err = (&lvmcluster.LVMClusterReconciler{
		Client:                           mgr.GetClient(),
		EventRecorder:                    mgr.GetEventRecorderFor("LVMClusterReconciler"),
		ClusterType:                      clusterType,
		Namespace:                        operatorNamespace,
		TopoLVMLeaderElectionPassthrough: leaderElectionConfig,
		EnableSnapshotting:               enableSnapshotting,
		VGManagerCommand:                 opts.vgManagerCommand,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create LVMCluster controller: %w", err)
	}

	if !snoCheck.IsSNO(cmd.Context()) {
		opts.SetupLog.Info("starting node-removal controller to observe node removal in MultiNode")
		if err = (&node.RemovalController{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to create NodeRemovalController controller: %w", err)
		}
	}

	if err = mgr.GetFieldIndexer().IndexField(context.Background(), &lvmv1alpha1.LVMVolumeGroupNodeStatus{}, "metadata.name", func(object client.Object) []string {
		return []string{object.GetName()}
	}); err != nil {
		return fmt.Errorf("unable to create name index on LVMVolumeGroupNodeStatus: %w", err)
	}

	if err = (&lvmv1alpha1.LVMCluster{}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create LVMCluster webhook: %w", err)
	}

	pvController := persistent_volume.NewPersistentVolumeReconciler(mgr.GetClient(), mgr.GetAPIReader(), mgr.GetEventRecorderFor("lvms-pv-controller"))
	if err := pvController.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create PersistentVolume controller: %w", err)
	}

	pvcController := persistent_volume_claim.NewPersistentVolumeClaimReconciler(mgr.GetClient(), mgr.GetEventRecorderFor("lvms-pvc-controller"))
	if err := pvcController.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create PersistentVolumeClaim controller: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	opts.SetupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}
