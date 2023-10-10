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

	"github.com/go-logr/logr"
	lvmv1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/dmsetup"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/wipefs"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	namespace := os.Getenv("POD_NAMESPACE")

	setupClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: opts.Scheme})
	if err != nil {
		return fmt.Errorf("unable to initialize setup client for pre-manager startup checks: %w", err)
	}

	if err := addTagToVGs(cmd.Context(), setupClient, lvm, nodeName, namespace); err != nil {
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
				os.Getenv("POD_NAMESPACE"): {},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
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
		Namespace:     namespace,
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

// addTagToVGs adds a lvms tag to the existing volume groups. This is a temporary logic that should be removed in v4.16.
func addTagToVGs(ctx context.Context, c client.Client, lvm lvm.LVM, nodeName string, namespace string) error {
	logger := log.FromContext(ctx)

	vgs, err := lvm.ListVGs()
	if err != nil {
		return fmt.Errorf("failed to list volume groups: %w", err)
	}

	lvmVolumeGroupList := &lvmv1.LVMVolumeGroupList{}
	err = c.List(ctx, lvmVolumeGroupList, &client.ListOptions{Namespace: namespace})
	if err != nil {
		return fmt.Errorf("failed to list LVMVolumeGroups: %w", err)
	}

	// If there is a matching LVMVolumeGroup CR, tag the existing volume group
	for _, vg := range vgs {
		tagged := false
		for _, lvmVolumeGroup := range lvmVolumeGroupList.Items {
			if vg.Name != lvmVolumeGroup.Name {
				continue
			}
			if lvmVolumeGroup.Spec.NodeSelector != nil {
				node := &corev1.Node{}
				err = c.Get(ctx, types.NamespacedName{Name: nodeName}, node)
				if err != nil {
					return fmt.Errorf("failed to get node %s: %w", nodeName, err)
				}

				matches, err := corev1helper.MatchNodeSelectorTerms(node, lvmVolumeGroup.Spec.NodeSelector)
				if err != nil {
					return fmt.Errorf("failed to match nodeSelector to node labels: %w", err)
				}
				if !matches {
					continue
				}
			}

			if err := lvm.AddTagToVG(vg.Name); err != nil {
				return err
			}
			tagged = true
		}
		if !tagged {
			logger.Info("skipping tagging volume group %s as there is no corresponding LVMVolumeGroup CR", vg.Name)
		}
	}

	return nil
}
