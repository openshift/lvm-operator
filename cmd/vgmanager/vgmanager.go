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
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/openshift/lvm-operator/internal/cluster"
	"github.com/openshift/lvm-operator/internal/controllers/constants"
	"github.com/openshift/lvm-operator/internal/controllers/lvmcluster/resource"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/dmsetup"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/wipefs"
	internalCSI "github.com/openshift/lvm-operator/internal/csi"
	"github.com/openshift/lvm-operator/internal/migration/tagging"
	"github.com/openshift/lvm-operator/internal/runner"
	"github.com/spf13/cobra"
	"github.com/topolvm/topolvm"
	"github.com/topolvm/topolvm/pkg/controller"
	"github.com/topolvm/topolvm/pkg/driver"
	topoLVMD "github.com/topolvm/topolvm/pkg/lvmd"
	"github.com/topolvm/topolvm/pkg/runners"
	"google.golang.org/grpc"
	"k8s.io/utils/ptr"

	"k8s.io/apimachinery/pkg/runtime"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/yaml"
)

const (
	DefaultDiagnosticsAddr = ":8443"
	DefaultProbeAddr       = ":8081"
	lvmConfig              = `
global {
	use_lvmlockd = 1
}

activation {
	udev_sync = 0
	udev_rules = 0
}`
)

var ErrConfigModified = errors.New("lvmd config file is modified")

type Options struct {
	Scheme   *runtime.Scheme
	SetupLog logr.Logger

	diagnosticsAddr     string
	healthProbeAddr     string
	enableSharedVolumes bool
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
			return run(cmd, args, opts)
		},
	}

	cmd.Flags().StringVar(
		&opts.diagnosticsAddr, "diagnosticsAddr", DefaultDiagnosticsAddr, "The address the diagnostics endpoint binds to.",
	)
	cmd.Flags().StringVar(
		&opts.healthProbeAddr, "health-probe-bind-address", DefaultProbeAddr, "The address the probe endpoint binds to.",
	)
	cmd.Flags().BoolVar(
		&opts.enableSharedVolumes, "enable-shared-volumes", true,
		"Enable using shared volumes. Enabling this will ensure sanlock, lvmlockd, and watchdog runs on every node, and vgs are created using the locks.",
	)
	return cmd
}

func run(cmd *cobra.Command, _ []string, opts *Options) error {
	ctx, cancelWithCause := context.WithCancelCause(cmd.Context())
	defer cancelWithCause(nil)
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	lvm := lvm.NewDefaultHostLVM()
	nodeName := os.Getenv("NODE_NAME")

	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("unable to get operatorNamespace: %w", err)
	}

	setupClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: opts.Scheme})
	if err != nil {
		return fmt.Errorf("unable to initialize setup client for pre-manager startup checks: %w", err)
	}

	if err := tagging.AddTagToVGs(ctx, setupClient, lvm, nodeName, operatorNamespace); err != nil {
		opts.SetupLog.Error(err, "failed to tag existing volume groups")
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

	var volumeGroupLock runner.VolumeGroupLock
	if opts.enableSharedVolumes {
		// create lvm.conf file
		err := os.WriteFile("/etc/lvm/lvm.conf", []byte(lvmConfig), 0644)
		if err != nil {
			return fmt.Errorf("could not write lvm.conf file: %w", err)
		}

		if err := mgr.Add(runner.NewSanlockRunner(nodeName)); err != nil {
			return fmt.Errorf("could not add sanlock runner: %w", err)
		}

		if err := mgr.Add(runner.NewLvmlockdRunner(nodeName)); err != nil {
			return fmt.Errorf("could not add lvmlockd runner: %w", err)
		}

		if volumeGroupLock, err = runner.NewVolumeGroupLock(mgr, nodeName, operatorNamespace); err != nil {
			return fmt.Errorf("could not create volume group lock: %w", err)
		} else if err := mgr.Add(volumeGroupLock); err != nil {
			return fmt.Errorf("could not add volume group lock: %w", err)
		}
	} else {
		volumeGroupLock = runner.NewNoneVolumeGroupLock()
	}

	lvmdConfig := &lvmd.Config{}
	if err := loadConfFile(ctx, lvmdConfig, lvmd.DefaultFileConfigPath); err != nil {
		opts.SetupLog.Error(err, "lvmd config could not be loaded, starting without topolvm components and attempting bootstrap")
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			return fmt.Errorf("unable to set up ready check: %w", err)
		}
	} else {
		topoLVMD.Containerized(true)
		lvclnt, vgclnt := topoLVMD.NewEmbeddedServiceClients(ctx, lvmdConfig.DeviceClasses, lvmdConfig.LvcreateOptionClasses)

		if err := controller.SetupLogicalVolumeReconcilerWithServices(mgr, mgr.GetClient(), nodeName, vgclnt, lvclnt); err != nil {
			return fmt.Errorf("unable to create LogicalVolumeReconciler: %w", err)
		}

		if err := mgr.Add(runners.NewMetricsExporter(vgclnt, mgr.GetClient(), nodeName)); err != nil { // adjusted signature
			return fmt.Errorf("could not add topolvm metrics: %w", err)
		}

		if err := os.MkdirAll(topolvm.DeviceDirectory, 0755); err != nil {
			return err
		}
		grpcServer := grpc.NewServer(grpc.UnaryInterceptor(ErrorLoggingInterceptor),
			grpc.SharedWriteBuffer(true),
			grpc.MaxConcurrentStreams(1),
			grpc.NumStreamWorkers(1))
		identityServer := driver.NewIdentityServer(func() (bool, error) {
			return true, nil
		})
		csi.RegisterIdentityServer(grpcServer, identityServer)

		registrationServer := internalCSI.NewRegistrationServer(
			constants.TopolvmCSIDriverName, registrationPath(), []string{"1.0.0"})
		registerapi.RegisterRegistrationServer(grpcServer, registrationServer)
		if err = mgr.Add(runners.NewGRPCRunner(grpcServer, pluginRegistrationSocketPath(), false)); err != nil {
			return fmt.Errorf("could not add grpc runner for registration server: %w", err)
		}

		nodeServer, err := driver.NewNodeServer(nodeName, vgclnt, lvclnt, mgr) // adjusted signature
		if err != nil {
			return fmt.Errorf("could not setup topolvm node server: %w", err)
		}
		csi.RegisterNodeServer(grpcServer, nodeServer)
		err = mgr.Add(runners.NewGRPCRunner(grpcServer, constants.DefaultCSISocket, false))
		if err != nil {
			return fmt.Errorf("could not add grpc runner for node server: %w", err)
		}
	}

	if err = (&vgmanager.Reconciler{
		Client:              mgr.GetClient(),
		EventRecorder:       mgr.GetEventRecorderFor(vgmanager.ControllerName),
		LVMD:                lvmd.DefaultConfigurator(),
		Scheme:              mgr.GetScheme(),
		LSBLK:               lsblk.NewDefaultHostLSBLK(),
		Wipefs:              wipefs.NewDefaultHostWipefs(),
		Dmsetup:             dmsetup.NewDefaultHostDmsetup(),
		LVM:                 lvm,
		NodeName:            nodeName,
		Namespace:           operatorNamespace,
		Filters:             filter.DefaultFilters,
		IsVolumeGroupLeader: volumeGroupLock.IsLeader,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller VGManager: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("unable to set up file watcher: %w", err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
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

		if err = watcher.Add(lvmd.DefaultFileConfigDir); err != nil {
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
	}()

	opts.SetupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	if errors.Is(context.Cause(ctx), ErrConfigModified) {
		opts.SetupLog.Info("restarting controller due to modified configuration")
		return run(cmd, nil, opts)
	} else if err := ctx.Err(); err != nil {
		opts.SetupLog.Error(err, "exiting abnormally")
		os.Exit(1)
	}

	return nil
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

func pluginRegistrationSocketPath() string {
	return fmt.Sprintf("%s/%s-reg.sock", constants.DefaultPluginRegistrationPath, constants.TopolvmCSIDriverName)
}
