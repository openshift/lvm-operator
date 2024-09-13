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
	"crypto/tls"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/openshift/lvm-operator/internal/cluster"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/dmsetup"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/wipefs"
	"github.com/openshift/lvm-operator/internal/tagging"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/runtime"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
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

	if err := tagging.AddTagToVGs(cmd.Context(), setupClient, lvm, nodeName, operatorNamespace); err != nil {
		opts.SetupLog.Error(err, "failed to tag existing volume groups")
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
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	if err := mgr.GetFieldIndexer().IndexField(cmd.Context(), &corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}

	if err = (&vgmanager.Reconciler{
		Client:        mgr.GetClient(),
		EventRecorder: mgr.GetEventRecorderFor(vgmanager.ControllerName),
		LVMD:          lvmd.DefaultConfigurator(),
		Scheme:        mgr.GetScheme(),
		LSBLK:         lsblk.NewDefaultHostLSBLK(),
		Wipefs:        wipefs.NewDefaultHostWipefs(),
		Dmsetup:       dmsetup.NewDefaultHostDmsetup(),
		LVM:           lvm,
		NodeName:      nodeName,
		Namespace:     operatorNamespace,
		Filters:       filter.DefaultFilters,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller VGManager: %w", err)
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
