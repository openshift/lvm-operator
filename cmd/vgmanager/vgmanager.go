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
	"github.com/spf13/cobra"
	topolvmClient "github.com/topolvm/topolvm/client"
	"github.com/topolvm/topolvm/controllers"
	"github.com/topolvm/topolvm/driver"
	topoLVMD "github.com/topolvm/topolvm/lvmd"
	"github.com/topolvm/topolvm/lvmd/command"
	"github.com/topolvm/topolvm/runners"
	"google.golang.org/grpc"

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
)

type Options struct {
	Scheme   *runtime.Scheme
	SetupLog logr.Logger

	diagnosticsAddr string
	healthProbeAddr string
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
	return cmd
}

func run(cmd *cobra.Command, _ []string, opts *Options) error {
	ctx, cancel := context.WithCancel(cmd.Context())
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
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	lvmdConfig := &lvmd.Config{}
	if err := loadConfFile(ctx, lvmdConfig, constants.LVMDDefaultFileConfigPath); err != nil {
		opts.SetupLog.Error(err, "lvmd config could not be loaded, starting without topolvm components and attempting bootstrap")
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			return fmt.Errorf("unable to set up ready check: %w", err)
		}
	} else {
		topoClient := topolvmClient.NewWrappedClient(mgr.GetClient())
		command.Containerized = true
		dcm := topoLVMD.NewDeviceClassManager(lvmdConfig.DeviceClasses)
		ocm := topoLVMD.NewLvcreateOptionClassManager(lvmdConfig.LvcreateOptionClasses)
		lvclnt, vgclnt := topoLVMD.NewEmbeddedServiceClients(ctx, dcm, ocm)

		lvController := controllers.NewLogicalVolumeReconcilerWithServices(topoClient, nodeName, vgclnt, lvclnt)

		if err := lvController.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to create LogicalVolumeReconciler: %w", err)
		}

		if err := mgr.Add(runners.NewMetricsExporter(vgclnt, topoClient, nodeName)); err != nil { // adjusted signature
			return fmt.Errorf("could not add topolvm metrics: %w", err)
		}

		if err := os.MkdirAll(driver.DeviceDirectory, 0755); err != nil {
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
		Client:        mgr.GetClient(),
		EventRecorder: mgr.GetEventRecorderFor(vgmanager.ControllerName),
		LVMD:          lvmd.NewFileConfigurator(mgr.GetClient(), operatorNamespace),
		Scheme:        mgr.GetScheme(),
		LSBLK:         lsblk.NewDefaultHostLSBLK(),
		Wipefs:        wipefs.NewDefaultHostWipefs(),
		Dmsetup:       dmsetup.NewDefaultHostDmsetup(),
		LVM:           lvm,
		NodeName:      nodeName,
		Namespace:     operatorNamespace,
		Filters:       filter.DefaultFilters,
		Shutdown:      cancel,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller VGManager: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
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

	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("unable to set up file watcher: %w", err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
		fileNotExist := false
		for {
			// check if file exists, otherwise the watcher errors
			_, err := os.Lstat(constants.LVMDDefaultFileConfigPath)
			if err != nil {
				if os.IsNotExist(err) {
					time.Sleep(100 * time.Millisecond)
					fileNotExist = true
				} else {
					opts.SetupLog.Error(err, "unable to check if lvmd config file exists")
				}
			} else {
				// This handles the first time the file is created through the configmap
				if fileNotExist {
					cancel()
				}
				err = watcher.Add(constants.LVMDDefaultFileConfigPath)
				if err != nil {
					opts.SetupLog.Error(err, "unable to add file path to watcher")
				}
				break
			}
		}
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Chmod) {
					opts.SetupLog.Info("lvmd config file is modified", "eventName", event.Name)
					cancel()
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
