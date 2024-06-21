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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/logpassthrough"
	"github.com/openshift/lvm-operator/v4/internal/controllers/node/removal"
	"github.com/openshift/lvm-operator/v4/internal/controllers/persistent-volume"
	"github.com/openshift/lvm-operator/v4/internal/controllers/persistent-volume-claim"
	internalCSI "github.com/openshift/lvm-operator/v4/internal/csi"
	"github.com/openshift/lvm-operator/v4/internal/migration/microlvms"
	"github.com/spf13/cobra"
	topolvmcontrollers "github.com/topolvm/topolvm/pkg/controller"
	"github.com/topolvm/topolvm/pkg/driver"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appsv1 "k8s.io/api/apps/v1"
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

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
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

	LogPassthroughOptions *logpassthrough.Options

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

	opts.LogPassthroughOptions = logpassthrough.NewOptions()
	opts.LogPassthroughOptions.BindFlags(cmd.Flags())

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

// NoManagedFields removes the managedFields from the object.
// This is used to reduce memory usage of the objects in the cache.
// This MUST NOT be used for SSA.
func NoManagedFields(i any) (any, error) {
	it, ok := i.(client.Object)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", i)
	}
	it.SetManagedFields(nil)
	return it, nil
}

func run(cmd *cobra.Command, _ []string, opts *Options) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

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

	leaderElectionConfig, err := leaderElectionResolver.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("unable to resolve leader election config: %w", err)
	}

	clusterType, err := cluster.NewTypeResolver(setupClient).GetType(ctx)
	if err != nil {
		return fmt.Errorf("unable to determine cluster type: %w", err)
	}

	enableSnapshotting := true
	vsc := &snapapi.VolumeSnapshotClassList{}
	if err := setupClient.List(ctx, vsc, &client.ListOptions{Limit: 1}); err != nil {
		// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
		if meta.IsNoMatchError(err) {
			opts.SetupLog.Info("VolumeSnapshotClasses do not exist on the cluster, ignoring")
			enableSnapshotting = false
		}
	}

	if err := microlvms.NewCleanup(setupClient, operatorNamespace).RemovePreMicroLVMSComponents(ctx); err != nil {
		return fmt.Errorf("failed to run pre 4.16 MicroLVMS cleanup: %w", err)
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
			DefaultTransform: NoManagedFields,
			ByObject: map[client.Object]cache.ByObject{
				&lvmv1alpha1.LVMCluster{}:               {Namespaces: map[string]cache.Config{operatorNamespace: {}}},
				&lvmv1alpha1.LVMVolumeGroup{}:           {Namespaces: map[string]cache.Config{operatorNamespace: {}}},
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{}: {Namespaces: map[string]cache.Config{operatorNamespace: {}}},
				&appsv1.Deployment{}:                    {Namespaces: map[string]cache.Config{operatorNamespace: {}}},
				&appsv1.DaemonSet{}:                     {Namespaces: map[string]cache.Config{operatorNamespace: {}}},
			},
		},
		HealthProbeBindAddress:        opts.healthProbeAddr,
		RetryPeriod:                   &leaderElectionConfig.RetryPeriod.Duration,
		LeaseDuration:                 &leaderElectionConfig.LeaseDuration.Duration,
		RenewDeadline:                 &leaderElectionConfig.RenewDeadline.Duration,
		LeaderElectionNamespace:       operatorNamespace,
		LeaderElectionID:              leaderElectionConfig.Name,
		LeaderElection:                !leaderElectionConfig.Disable,
		LeaderElectionReleaseOnCancel: true,
		GracefulShutdownTimeout:       ptr.To(time.Duration(-1)),
		PprofBindAddress:              ":9099",
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// register controllers
	if err = (&lvmcluster.Reconciler{
		Client:                           mgr.GetClient(),
		EventRecorder:                    mgr.GetEventRecorderFor("LVMClusterReconciler"),
		ClusterType:                      clusterType,
		Namespace:                        operatorNamespace,
		TopoLVMLeaderElectionPassthrough: leaderElectionConfig,
		EnableSnapshotting:               enableSnapshotting,
		LogPassthroughOptions:            opts.LogPassthroughOptions,
		VGManagerCommand:                 opts.vgManagerCommand,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create LVMCluster controller: %w", err)
	}

	opts.SetupLog.Info("starting node-removal controller")
	if err = removal.NewReconciler(mgr.GetClient(), operatorNamespace).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create NodeRemovalController controller: %w", err)
	}

	if err = (&lvmv1alpha1.LVMCluster{}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create LVMCluster webhook: %w", err)
	}

	pvController := persistent_volume.NewReconciler(mgr.GetClient(), mgr.GetEventRecorderFor("lvms-pv-controller"))
	if err := pvController.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create PersistentVolume controller: %w", err)
	}

	pvcController := persistent_volume_claim.NewReconciler(mgr.GetClient(), mgr.GetEventRecorderFor("lvms-pvc-controller"))
	if err := pvcController.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create PersistentVolumeClaim controller: %w", err)
	}

	// register TopoLVM controllers
	if err := topolvmcontrollers.SetupNodeReconciler(mgr, mgr.GetClient(), false); err != nil {
		return fmt.Errorf("unable to create TopoLVM Node controller: %w", err)
	}

	if err := topolvmcontrollers.SetupPersistentVolumeClaimReconciler(mgr, mgr.GetClient(), mgr.GetAPIReader()); err != nil {
		return fmt.Errorf("unable to create TopoLVM PVC controller: %w", err)
	}

	grpcServer := grpc.NewServer(
		grpc.SharedWriteBuffer(true),
		grpc.MaxConcurrentStreams(1),
		grpc.NumStreamWorkers(1))
	identityServer := driver.NewIdentityServer(func() (bool, error) {
		return true, nil
	})
	csi.RegisterIdentityServer(grpcServer, identityServer)
	controllerSever, err := driver.NewControllerServer(mgr, driver.ControllerServerSettings{
		MinimumAllocationSettings: driver.MinimumAllocationSettings{
			Filesystem: map[string]driver.Quantity{
				string(lvmv1alpha1.FilesystemTypeExt4): driver.Quantity(resource.MustParse(constants.DefaultMinimumAllocationSizeExt4)),
				string(lvmv1alpha1.FilesystemTypeXFS):  driver.Quantity(resource.MustParse(constants.DefaultMinimumAllocationSizeXFS)),
			},
			Block: driver.Quantity(resource.MustParse(constants.DefaultMinimumAllocationSizeBlock)),
		},
	})
	if err != nil {
		return err
	}
	csi.RegisterControllerServer(grpcServer, controllerSever)

	if err = mgr.Add(internalCSI.NewGRPCRunner(grpcServer, constants.DefaultCSISocket, false)); err != nil {
		return err
	}

	if err := mgr.Add(internalCSI.NewProvisioner(mgr, internalCSI.ProvisionerOptions{
		DriverName:          constants.TopolvmCSIDriverName,
		CSIEndpoint:         constants.DefaultCSISocket,
		CSIOperationTimeout: 10 * time.Second,
	})); err != nil {
		return fmt.Errorf("unable to create CSI Provisioner: %w", err)
	}

	if enableSnapshotting {
		if err := mgr.Add(internalCSI.NewSnapshotter(mgr, internalCSI.SnapshotterOptions{
			DriverName:          constants.TopolvmCSIDriverName,
			CSIEndpoint:         constants.DefaultCSISocket,
			CSIOperationTimeout: 10 * time.Second,
		})); err != nil {
			return fmt.Errorf("unable to create CSI Snapshotter: %w", err)
		}
	}

	if err := mgr.Add(internalCSI.NewResizer(mgr, internalCSI.ProvisionerOptions{
		DriverName:          constants.TopolvmCSIDriverName,
		CSIEndpoint:         constants.DefaultCSISocket,
		CSIOperationTimeout: 10 * time.Second,
	})); err != nil {
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		if err := readyCheck(mgr)(req); err != nil {
			return err
		}
		return mgr.GetWebhookServer().StartedChecker()(req)
	}); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	c := make(chan os.Signal, 2)
	signal.Notify(c, []os.Signal{os.Interrupt, syscall.SIGTERM}...)
	go func() {
		<-c
		cancel()
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	opts.SetupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}

// readyCheck returns a healthz.Checker that verifies the operator is ready
func readyCheck(mgr manager.Manager) healthz.Checker {
	return func(req *http.Request) error {
		// Perform various checks here to determine if the operator is ready
		if !mgr.GetCache().WaitForCacheSync(req.Context()) {
			return fmt.Errorf("informer cache not synced and thus not ready")
		}
		return nil
	}
}
