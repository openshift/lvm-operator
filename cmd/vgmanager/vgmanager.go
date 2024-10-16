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

package vgmanager

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/resource"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/dmsetup"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvmd"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/util"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/wipefs"
	icsi "github.com/openshift/lvm-operator/v4/internal/csi"
	runner "github.com/openshift/lvm-operator/v4/internal/locking"
	"github.com/spf13/cobra"
	"github.com/topolvm/topolvm/pkg/controller"
	"github.com/topolvm/topolvm/pkg/driver"
	topoLVMD "github.com/topolvm/topolvm/pkg/lvmd"
	"github.com/topolvm/topolvm/pkg/runners"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"k8s.io/apimachinery/pkg/api/meta"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/yaml"
)

const (
	DefaultDiagnosticsAddr = ":8443"
	DefaultHealthProbeAddr = ":8081"
)

var ErrConfigModified = errors.New("lvmd config file is modified")
var ErrNoDeviceClassesAvailable = errors.New("no device classes in lvmd.yaml configured, can not startup correctly")
var ErrCSIPluginNotYetRegistered = errors.New("CSI plugin not yet registered")

type Options struct {
	Scheme   *runtime.Scheme
	SetupLog logr.Logger

	diagnosticsAddr string
	healthProbeAddr string

	enableSharedVolumes bool
	Metrics             *connection.ExtendedCSIMetricsManager
}

// NewCmd creates a new CLI command
func NewCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "vgmanager",
		Short:         "Commands for running vgmanager",
		Long:          `Controller reconciling LVMVolumeGroup on individual nodes`,
		SilenceErrors: false,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithFileLock(cmd, args, opts)
		},
	}

	cmd.Flags().StringVar(
		&opts.diagnosticsAddr, "diagnosticsAddr", DefaultDiagnosticsAddr, "The address the diagnostics endpoint binds to.",
	)
	cmd.Flags().StringVar(
		&opts.healthProbeAddr, "health-probe-bind-address", DefaultHealthProbeAddr, "The address the probe endpoint binds to.",
	)
	cmd.Flags().BoolVar(
		&opts.enableSharedVolumes, "enable-shared-volumes", false,
		"Enable using shared volumes. Enabling this will ensure sanlock, lvmlockd, and watchdog runs on every node, and vgs are created using the locks.",
	)
	return cmd
}

func runWithFileLock(cmd *cobra.Command, args []string, opts *Options) error {
	lock, err := util.NewFileLock("vgmanager")
	if err != nil {
		return fmt.Errorf("unable to create lock: %w", err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			opts.SetupLog.Error(err, "unable to close file lock")
		}
	}()

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	if err := lock.WaitForLock(ctx); err != nil {
		return fmt.Errorf("unable to acquire lock: %w", err)
	}

	return run(cmd, args, opts)
}

func run(cmd *cobra.Command, _ []string, opts *Options) error {
	ctx, cancelWithCause := context.WithCancelCause(cmd.Context())
	defer cancelWithCause(nil)
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	nodeName := os.Getenv("NODE_NAME")

	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("unable to get operatorNamespace: %w", err)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: opts.Scheme,
		Metrics: metricsserver.Options{
			BindAddress:    opts.diagnosticsAddr,
			SecureServing:  true,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) {
					opts.SetupLog.Info("disabling http/2")
					c.NextProtos = []string{"http/1.1"}
				},
			},
		},
		WebhookServer: &webhook.DefaultServer{Options: webhook.Options{
			Port: 9443,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) {
					opts.SetupLog.Info("disabling http/2")
					c.NextProtos = []string{"http/1.1"}
				},
			},
		}},
		HealthProbeBindAddress: opts.healthProbeAddr,
		LeaderElection:         false,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				operatorNamespace: {},
			},
		},
		GracefulShutdownTimeout: ptr.To(time.Duration(-1)),
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	type registration struct {
		icsi.CheckableRegistrationServer
		registrationPath string
	}
	var registrations []registration

	var volumeGroupLock runner.VolumeGroupLock
	if opts.enableSharedVolumes {
		if volumeGroupLock, err = runner.NewVolumeGroupLock(ctx, mgr, nodeName, operatorNamespace); err != nil {
			return fmt.Errorf("could not create volume group lock: %w", err)
		} else if err := mgr.Add(volumeGroupLock); err != nil {
			return fmt.Errorf("could not add volume group lock: %w", err)
		}
		reg := registration{
			CheckableRegistrationServer: icsi.NewRegistrationServer(
				cancelWithCause,
				constants.KubeSANCSIDriverName,
				kubeSANRegistrationPath(),
				[]string{"1.0.0"},
			),
			registrationPath: kubeSANpluginRegistrationSocketPath(),
		}
		registrations = append(registrations, reg)
		registrationGrpcServer := newGRPCServer()
		registerapi.RegisterRegistrationServer(registrationGrpcServer, reg)
		if err = mgr.Add(icsi.NewGRPCRunner(registrationGrpcServer, reg.registrationPath, false)); err != nil {
			return fmt.Errorf("could not add grpc runner for reg server: %w", err)
		}

		if err := mgr.Add(icsi.NewProvisioner(mgr, icsi.ProvisionerOptions{
			DriverName:          constants.KubeSANCSIDriverName,
			CSIEndpoint:         constants.ControllerCSILocalPath,
			CSIOperationTimeout: 10 * time.Second,
			NodeDeployment: &icsi.NodeDeployment{
				NodeName:        nodeName,
				NodeCSIEndpoint: kubeSANRegistrationPath(),
			},
			ExtraCreateMetadata: true,
			Metrics:             opts.Metrics,
		})); err != nil {
			return fmt.Errorf("unable to create CSI Provisioner: %w", err)
		}

		enableSnapshotting := true
		vsc := &snapapi.VolumeSnapshotClassList{}
		if err := mgr.GetClient().List(ctx, vsc, &client.ListOptions{Limit: 1}); err != nil {
			// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
			if meta.IsNoMatchError(err) {
				opts.SetupLog.Info("VolumeSnapshotClasses do not exist on the cluster, ignoring")
				enableSnapshotting = false
			}
		}

		if enableSnapshotting {
			if err := mgr.Add(icsi.NewSnapshotter(mgr, icsi.SnapshotterOptions{
				DriverName:          constants.KubeSANCSIDriverName,
				CSIEndpoint:         constants.ControllerCSILocalPath,
				CSIOperationTimeout: 10 * time.Second,
				LeaderElection:      true,
				ExtraCreateMetadata: true,
			})); err != nil {
				return fmt.Errorf("unable to create CSI Snapshotter: %w", err)
			}
		}
	} else {
		volumeGroupLock = runner.NewNoneVolumeGroupLock()
	}

	lvmdConfig := &lvmd.Config{}
	if err := loadConfFile(ctx, lvmdConfig, lvmd.DefaultFileConfigPath); err != nil {
		opts.SetupLog.Error(err, "lvmd config could not be loaded, starting without topolvm components and attempting bootstrap")
	} else {
		lvclnt, vgclnt := topoLVMD.NewEmbeddedServiceClients(ctx, lvmdConfig.DeviceClasses, lvmdConfig.LvcreateOptionClasses)

		if err := controller.SetupLogicalVolumeReconcilerWithServices(mgr, mgr.GetClient(), nodeName, vgclnt, lvclnt); err != nil {
			return fmt.Errorf("unable to create LogicalVolumeReconciler: %w", err)
		}

		if err := mgr.Add(runners.NewMetricsExporter(vgclnt, mgr.GetClient(), nodeName)); err != nil { // adjusted signature
			return fmt.Errorf("could not add topolvm metrics: %w", err)
		}

		reg := registration{
			CheckableRegistrationServer: icsi.NewRegistrationServer(
				cancelWithCause,
				constants.TopolvmCSIDriverName,
				registrationPath(),
				[]string{"1.0.0"},
			),
			registrationPath: topoLVMpluginRegistrationSocketPath(),
		}

		csiGrpcServer := newGRPCServer()
		grpc_health_v1.RegisterHealthServer(csiGrpcServer, icsi.NewHealthServer(func(ctx context.Context) error {
			return readyCheck(ctx, mgr, lvmdConfig, reg)
		}))
		identityServer := driver.NewIdentityServer(func() (bool, error) {
			return true, nil
		})
		csi.RegisterIdentityServer(csiGrpcServer, identityServer)

		nodeServer, err := driver.NewNodeServer(nodeName, vgclnt, lvclnt, mgr) // adjusted signature
		if err != nil {
			return fmt.Errorf("could not setup topolvm node server: %w", err)
		}
		csi.RegisterNodeServer(csiGrpcServer, nodeServer)
		err = mgr.Add(icsi.NewGRPCRunner(csiGrpcServer, constants.DefaultCSISocket, false))
		if err != nil {
			return fmt.Errorf("could not add grpc runner for node server: %w", err)
		}

		registrations = append(registrations, reg)
		registrationGrpcServer := newGRPCServer()
		grpc_health_v1.RegisterHealthServer(registrationGrpcServer, icsi.NewHealthServer(icsi.AlwaysHealthy))
		registerapi.RegisterRegistrationServer(registrationGrpcServer, reg)
		if err = mgr.Add(icsi.NewGRPCRunner(registrationGrpcServer, reg.registrationPath, false)); err != nil {
			return fmt.Errorf("could not add grpc runner for reg server: %w", err)
		}
	}

	if err = (&vgmanager.Reconciler{
		Client:           mgr.GetClient(),
		EventRecorder:    mgr.GetEventRecorderFor(vgmanager.ControllerName),
		LVMD:             lvmd.DefaultConfigurator(),
		Scheme:           mgr.GetScheme(),
		LSBLK:            lsblk.NewDefaultHostLSBLK(),
		Wipefs:           wipefs.NewDefaultHostWipefs(),
		Dmsetup:          dmsetup.NewDefaultHostDmsetup(),
		LVM:              lvm.NewDefaultHostLVM(),
		NodeName:         nodeName,
		Namespace:        operatorNamespace,
		Filters:          filter.DefaultFilters,
		SymlinkResolveFn: filepath.EvalSymlinks,
		IsSharedVGLeader: volumeGroupLock.IsLeader,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller VGManager: %w", err)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", func(req *http.Request) error {
		for _, rs := range registrations {
			if !rs.Registered() {
				log.FromContext(req.Context()).Error(ErrCSIPluginNotYetRegistered, "not healthy")
				return ErrCSIPluginNotYetRegistered
			}
		}
		if opts.enableSharedVolumes {
			log.FromContext(req.Context()).V(1).Info("shared volumes enabled, skipping lvmd health check")
			return nil
		}
		if len(lvmdConfig.DeviceClasses) == 0 {
			log.FromContext(req.Context()).Error(ErrNoDeviceClassesAvailable, "not healthy")
			return ErrNoDeviceClassesAvailable
		}
		return nil
	}); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("unable to set up file watcher: %w", err)
	}
	defer watcher.Close()
	// Start listening for events on TopoLVM files.
	go watchTopoLVMAndNotify(opts, cancelWithCause, watcher)

	opts.SetupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	if errors.Is(context.Cause(ctx), ErrConfigModified) {
		opts.SetupLog.Info("restarting controller due to modified configuration")
		return run(cmd, nil, opts)
	} else if errors.Is(context.Cause(ctx), icsi.ErrPluginRegistrationFailed) {
		opts.SetupLog.Error(context.Cause(ctx), "restarting due to failed plugin registration")
		return run(cmd, nil, opts)
	} else if err := ctx.Err(); err != nil {
		opts.SetupLog.Error(err, "exiting abnormally")
		os.Exit(1)
	}

	return nil
}

// watchTopoLVMAndNotify watches for changes to the lvmd config file and cancels the context if it changes.
// This is used to restart the manager when the lvmd config file changes.
// This is a blocking function and should be run in a goroutine.
// If the directory does not exist, it will be created to make it possible to watch for changes.
// If the watch determines that the lvmd config file has been modified, it will cancel the context with the ErrConfigModified error.
func watchTopoLVMAndNotify(opts *Options, cancelWithCause context.CancelCauseFunc, watcher *fsnotify.Watcher) {
	// check if file exists, otherwise the watcher errors
	fi, err := os.Stat(lvmd.DefaultFileConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(lvmd.DefaultFileConfigDir, 0755); err != nil {
				opts.SetupLog.Error(err, "unable to create lvmd config directory when it did not exist before")
			}
		} else {
			opts.SetupLog.Error(err, "unable to check if lvmd config directory exists")
			cancelWithCause(err)
		}
	} else if !fi.IsDir() {
		opts.SetupLog.Error(err, "expected lvmd config directory is not a directory")
		cancelWithCause(err)
	}

	err = watcher.Add(lvmd.DefaultFileConfigDir)
	if err != nil {
		opts.SetupLog.Error(err, "unable to add file path to watcher")
	}
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name != lvmd.DefaultFileConfigPath {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Chmod) {
				opts.SetupLog.Info("lvmd config file is modified", "eventName", event.Name)
				cancelWithCause(fmt.Errorf("%w: %s", ErrConfigModified, event.Name))
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			opts.SetupLog.Error(err, "file watcher error")
		}
	}
}

func loadConfFile(ctx context.Context, config *lvmd.Config, cfgFilePath string) error {
	b, err := os.ReadFile(cfgFilePath)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(b, config)
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("configuration file loaded",
		"device_classes", config.DeviceClasses,
		"file_name", cfgFilePath,
	)
	return nil
}

func ErrorLoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	resp, err = handler(ctx, req)
	if err != nil {
		ctrl.Log.Error(err, "error on grpc call", "method", info.FullMethod)
	}
	return resp, err
}

func registrationPath() string {
	return fmt.Sprintf("%splugins/%s/node/csi-topolvm.sock", resource.GetAbsoluteKubeletPath(constants.CSIKubeletRootDir), constants.TopolvmCSIDriverName)
}

func kubeSANRegistrationPath() string {
	return fmt.Sprintf("%splugins/%s/socket", resource.GetAbsoluteKubeletPath(constants.CSIKubeletRootDir), constants.KubeSANCSIDriverName)
}

func topoLVMpluginRegistrationSocketPath() string {
	return fmt.Sprintf("%s/%s-reg.sock", constants.DefaultPluginRegistrationPath, constants.TopolvmCSIDriverName)
}

func kubeSANpluginRegistrationSocketPath() string {
	return fmt.Sprintf("%s/%s-reg.sock", constants.DefaultPluginRegistrationPath, constants.KubeSANCSIDriverName)
}

func readyCheckHealthzChecker(
	mgr manager.Manager,
	config *lvmd.Config,
	server icsi.CheckableRegistrationServer,
) healthz.Checker {
	return func(req *http.Request) error {
		return readyCheck(req.Context(), mgr, config, server)
	}
}

func readyCheck(
	ctx context.Context,
	mgr manager.Manager,
	config *lvmd.Config,
	server icsi.CheckableRegistrationServer,
) error {
	logger := log.FromContext(ctx)
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		return fmt.Errorf("informer cache not synced and thus not ready")
	}
	if len(config.DeviceClasses) == 0 {
		logger.Error(ErrNoDeviceClassesAvailable, "not ready")
		return ErrNoDeviceClassesAvailable
	}
	if !server.Registered() {
		logger.Error(ErrCSIPluginNotYetRegistered, "not ready")
		return ErrCSIPluginNotYetRegistered
	}
	return nil
}

// newGRPCServer returns a new grpc.Server with the following settings:
// UnaryInterceptor: ErrorLoggingInterceptor, to log errors on grpc calls
// SharedWriteBuffer: true, to share write buffer between all connections, saving memory
// 2 streams for one core each (vgmanager is optimized for 1 hyperthreaded core)
// 2 workers for one core each (vgmanager is optimized for 1 hyperthreaded core)
// We technically could use 1 worker / 1 stream, but that would make the goroutine
// switch threads more often, which is less efficient.
func newGRPCServer() *grpc.Server {
	return grpc.NewServer(grpc.UnaryInterceptor(ErrorLoggingInterceptor),
		grpc.SharedWriteBuffer(true),
		grpc.MaxConcurrentStreams(2),
		grpc.NumStreamWorkers(2),
	)
}
