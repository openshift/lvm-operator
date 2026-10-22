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
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const networkPolicyManagerName = "networkPolicies"

func NetworkPolicies() Manager {
	return networkPolicyManager{}
}

type networkPolicyManager struct{}

var _ Manager = networkPolicyManager{}

func (n networkPolicyManager) GetName() string {
	return networkPolicyManagerName
}

//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;delete

func (n networkPolicyManager) EnsureCreated(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", n.GetName())

	// Allow policies first, then default-deny — avoids transient traffic blocking during
	// initial deployment or upgrade.
	policies := allNetworkPolicies(r.GetNamespace())
	for _, template := range policies {
		np := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      template.Name,
				Namespace: template.Namespace,
			},
		}

		result, err := cutil.CreateOrUpdate(ctx, r, np, func() error {
			np.Spec = template.Spec
			labels.SetManagedLabels(r.Scheme(), np, lvmCluster)
			return ctrl.SetControllerReference(lvmCluster, np, r.Scheme())
		})
		if err != nil {
			return fmt.Errorf("%s failed to reconcile NetworkPolicy %s: %w", n.GetName(), template.Name, err)
		}
		if result != cutil.OperationResultNone {
			logger.V(2).Info("NetworkPolicy applied to cluster", "operation", result, "name", template.Name)
		}
	}

	return nil
}

func (n networkPolicyManager) EnsureDeleted(r Reconciler, ctx context.Context, _ *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", n.GetName())

	// Reverse order: delete default-deny first so allow policies remain active
	// while the operator still needs API server egress for subsequent deletes.
	policies := allNetworkPolicies(r.GetNamespace())
	for i := len(policies) - 1; i >= 0; i-- {
		template := policies[i]
		np := &networkingv1.NetworkPolicy{}
		name := types.NamespacedName{Name: template.Name, Namespace: template.Namespace}

		if err := r.Get(ctx, name, np); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to get NetworkPolicy %s: %w", template.Name, err)
			}
			continue
		}

		if !np.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("NetworkPolicy %s is still present, waiting for deletion", template.Name)
		}

		if err := r.Delete(ctx, np); err != nil {
			return fmt.Errorf("failed to delete NetworkPolicy %s: %w", template.Name, err)
		}
		logger.Info("initiated NetworkPolicy deletion", "name", template.Name)
	}

	return nil
}

// allNetworkPolicies returns NP definitions in creation order: allow policies
// first, default-deny last.
func allNetworkPolicies(namespace string) []networkingv1.NetworkPolicy {
	return []networkingv1.NetworkPolicy{
		operatorAllowPolicy(namespace),
		vgManagerAllowPolicy(namespace),
		defaultDenyPolicy(namespace),
	}
}

func defaultDenyPolicy(namespace string) networkingv1.NetworkPolicy {
	return networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvms-default-deny",
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
}

func operatorAllowPolicy(namespace string) networkingv1.NetworkPolicy {
	return networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvms-operator-allow",
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					constants.AppKubernetesNameLabel: constants.ManagedByLabelVal,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						networkPolicyPort(corev1.ProtocolTCP, 9443),
					},
				},
				{
					Ports: []networkingv1.NetworkPolicyPort{
						networkPolicyPort(corev1.ProtocolTCP, 8443),
					},
				},
			},
			Egress: commonEgressRules(),
		},
	}
}

func vgManagerAllowPolicy(namespace string) networkingv1.NetworkPolicy {
	return networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvms-vg-manager-allow",
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					constants.AppKubernetesNameLabel: constants.VGManagerLabelVal,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						networkPolicyPort(corev1.ProtocolTCP, 8443),
					},
				},
			},
			Egress: commonEgressRules(),
		},
	}
}

func commonEgressRules() []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{
		{
			Ports: []networkingv1.NetworkPolicyPort{
				networkPolicyPort(corev1.ProtocolTCP, 6443),
			},
		},
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "openshift-dns",
						},
					},
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dns.operator.openshift.io/daemonset-dns": "default",
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				networkPolicyPort(corev1.ProtocolTCP, 5353),
				networkPolicyPort(corev1.ProtocolUDP, 5353),
			},
		},
	}
}

func networkPolicyPort(protocol corev1.Protocol, port int) networkingv1.NetworkPolicyPort {
	p := intstr.FromInt32(int32(port))
	return networkingv1.NetworkPolicyPort{
		Protocol: &protocol,
		Port:     &p,
	}
}
