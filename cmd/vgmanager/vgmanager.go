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
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/lvm-operator/internal/cluster"
	"github.com/openshift/lvm-operator/internal/controllers/constants"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/dmsetup"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/wipefs"
	internalCSI "github.com/openshift/lvm-operator/internal/csi"
	"github.com/openshift/lvm-operator/internal/tagging"
	"github.com/spf13/cobra"
	"github.com/topolvm/topolvm"
	"github.com/topolvm/topolvm/driver"
	"github.com/topolvm/topolvm/lvmd/proto"
	"github.com/topolvm/topolvm/runners"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/container-storage-interface/spec/lib/go/csi"

	topolvmClient "github.com/topolvm/topolvm/client"
	"github.com/topolvm/topolvm/controllers"
	topoLVMD "github.com/topolvm/topolvm/lvmd"
	"github.com/topolvm/topolvm/lvmd/command"

	"k8s.io/apimachinery/pkg/runtime"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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
		PprofBindAddress: ":9099",
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

		// Add health checker to manager
		checker := runners.NewChecker(checkFreeBytes(vgclnt), 10*time.Second) // adjusted signature
		if err := mgr.Add(checker); err != nil {
			return fmt.Errorf("could not add free bytes heealth check: %w", err)
		}

		if err := mgr.Add(runners.NewMetricsExporter(vgclnt, topoClient, nodeName)); err != nil { // adjusted signature
			return fmt.Errorf("could not add topolvm metrics: %w", err)
		}

		if err := os.MkdirAll(driver.DeviceDirectory, 0755); err != nil {
			return err
		}
		grpcServer := grpc.NewServer(grpc.UnaryInterceptor(ErrorLoggingInterceptor))
		identityServer := driver.NewIdentityServer(checker.Ready)
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

		if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
			// replicates behavior of https://github.com/kubernetes-csi/livenessprobe/blob/master/cmd/livenessprobe/main.go#L61-L93
			probe, err := identityServer.Probe(ctx, &csi.ProbeRequest{})
			if !probe.Ready.GetValue() {
				return fmt.Errorf("CSI Driver responded but is not ready")
			}
			if err != nil {
				return fmt.Errorf("CSI Identity Server health check failed: %w", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("unable to set up ready check: %w", err)
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
func checkFreeBytes(clnt proto.VGServiceClient) func() error {
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := clnt.GetFreeBytes(ctx, &proto.GetFreeBytesRequest{DeviceClass: topolvm.DefaultDeviceClassName})

		return err
	}
}

func ErrorLoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	resp, err = handler(ctx, req)
	if err != nil {
		ctrl.Log.Error(err, "error on grpc call", "method", info.FullMethod)
	}
	return resp, err
}

func registrationPath() string {
	return fmt.Sprintf("%splugins/%s/node/csi-topolvm.sock", getAbsoluteKubeletPath(constants.CSIKubeletRootDir), constants.TopolvmCSIDriverName)
}

func pluginRegistrationSocketPath() string {
	return fmt.Sprintf("%s/%s-reg.sock", constants.DefaultPluginRegistrationPath, constants.TopolvmCSIDriverName)
}
func getAbsoluteKubeletPath(name string) string {
	if strings.HasSuffix(name, "/") {
		return name
	} else {
		return name + "/"
	}
}
