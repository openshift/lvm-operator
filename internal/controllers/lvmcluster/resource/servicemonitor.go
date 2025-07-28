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

package resource

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func ServiceMonitor() Manager {
	return serviceMonitorManager{}
}

type serviceMonitorManager struct{}

var _ Manager = serviceMonitorManager{}

const monitorName = "lvms-operator-metrics-monitor"

func (s serviceMonitorManager) GetName() string {
	return monitorName
}

func (s serviceMonitorManager) EnsureCreated(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	isPrometheusAvailable, err := s.IsPrometheusAvailable(r, ctx)
	if err != nil {
		return fmt.Errorf("failed to check if Prometheus is available: %w", err)
	}

	if !isPrometheusAvailable {
		logger.V(2).Info("Prometheus CRD not available, skipping ServiceMonitor creation")
		return nil
	}

	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.GetName(),
			Namespace: r.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/name":    "lvms-operator",
				"app.kubernetes.io/compose": "metrics",
				"app.kubernetes.io/part-of": "lvms-provisioner",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Path:            "/metrics",
					Port:            "https",
					Scheme:          "https",
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							ServerName: ptr.To(fmt.Sprintf("lvms-operator-metrics-service.%s.svc", r.GetNamespace())),
						},
						CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
					},
				},
				{
					Path:            "/metrics",
					Port:            "vg-manager-https",
					Scheme:          "https",
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							ServerName: ptr.To(fmt.Sprintf("vg-manager-metrics-service.%s.svc", r.GetNamespace())),
						},
						CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/compose": "metrics",
				},
			},
		},
	}

	// Create or update the ServiceMonitor
	result, err := cutil.CreateOrUpdate(ctx, r, serviceMonitor, func() error {
		// Set managed labels
		labels.SetManagedLabels(r.Scheme(), serviceMonitor, lvmCluster)
		return nil
	})

	if err != nil {
		return fmt.Errorf("%s failed to reconcile: %w", s.GetName(), err)
	}

	if result != cutil.OperationResultNone {
		logger.V(2).Info("ServiceMonitor applied to cluster", "operation", result, "name", serviceMonitor.Name)
	}

	return nil
}

func (s serviceMonitorManager) EnsureDeleted(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	isPrometheusAvailable, err := s.IsPrometheusAvailable(r, ctx)
	if err != nil {
		return fmt.Errorf("failed to check if Prometheus is available: %w", err)
	}

	if !isPrometheusAvailable {
		logger.V(2).Info("Prometheus CRD not available, skipping ServiceMonitor deletion")
		return nil
	}

	name := types.NamespacedName{Name: s.GetName(), Namespace: r.GetNamespace()}
	serviceMonitor := &monitoringv1.ServiceMonitor{}

	if err := r.Get(ctx, name, serviceMonitor); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to get ServiceMonitor: %w", err)
		}
		// ServiceMonitor doesn't exist, nothing to delete
		return nil
	}

	if !serviceMonitor.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("ServiceMonitor %s is still present, waiting for deletion", serviceMonitor.Name)
	}

	if err := r.Delete(ctx, serviceMonitor); err != nil {
		return fmt.Errorf("failed to delete ServiceMonitor %s: %w", serviceMonitor.Name, err)
	}

	logger.Info("initiated ServiceMonitor deletion")
	return nil
}

func (s serviceMonitorManager) IsPrometheusAvailable(r Reconciler, ctx context.Context) (bool, error) {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	logger.V(2).Info("Checking if PrometheusCRD is available")
	// check if the ServiceMonitor CRD is available
	if err := r.Get(ctx, types.NamespacedName{Name: "servicemonitors.monitoring.coreos.com"}, &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return false, fmt.Errorf("failed to get ServiceMonitor CRD: %w", err)
		}
		return false, nil
	}

	return true, nil
}
