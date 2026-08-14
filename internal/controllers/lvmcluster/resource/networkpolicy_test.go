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
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func newTestSchemeWithNetworking(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := newTestScheme(t)
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding networkingv1 to scheme: %v", err)
	}
	return scheme
}

func newFakeNetworkPolicyReconciler(t *testing.T) *fakeReconciler {
	t.Helper()
	scheme := newTestSchemeWithNetworking(t)
	return &fakeReconciler{
		Client:    fake.NewClientBuilder().WithScheme(scheme).Build(),
		scheme:    scheme,
		namespace: "default",
	}
}

func TestNetworkPolicy_EnsureCreated(t *testing.T) {
	r := newFakeNetworkPolicyReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))
	cluster := testCluster()

	np := networkPolicyManager{}
	if err := np.EnsureCreated(r, ctx, cluster); err != nil {
		t.Fatalf("EnsureCreated returned error: %v", err)
	}

	// Verify all 3 NetworkPolicies were created
	for _, name := range []string{
		"lvms-operator-allow",
		"lvms-vg-manager-allow",
		"lvms-default-deny",
	} {
		got := &networkingv1.NetworkPolicy{}
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, got); err != nil {
			t.Errorf("expected NetworkPolicy %s to exist: %v", name, err)
		}
		// Verify managed labels are set
		if got.Labels[labels.OwnedByName] != "test-lvmcluster" {
			t.Errorf("NetworkPolicy %s: expected owned-by label %s, got %s", name, "test-lvmcluster", got.Labels[labels.OwnedByName])
		}
	}
}

func TestNetworkPolicy_DefaultDenySpec(t *testing.T) {
	r := newFakeNetworkPolicyReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))
	cluster := testCluster()

	np := networkPolicyManager{}
	if err := np.EnsureCreated(r, ctx, cluster); err != nil {
		t.Fatalf("EnsureCreated returned error: %v", err)
	}

	got := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: "lvms-default-deny", Namespace: "default"}, got); err != nil {
		t.Fatalf("failed to get default-deny NP: %v", err)
	}

	// podSelector should be empty (all pods)
	if len(got.Spec.PodSelector.MatchLabels) != 0 {
		t.Errorf("expected empty podSelector, got %v", got.Spec.PodSelector.MatchLabels)
	}

	// policyTypes should be both Ingress and Egress
	if len(got.Spec.PolicyTypes) != 2 {
		t.Fatalf("expected 2 policyTypes, got %d", len(got.Spec.PolicyTypes))
	}

	// No ingress or egress rules
	if len(got.Spec.Ingress) != 0 {
		t.Errorf("expected no ingress rules, got %d", len(got.Spec.Ingress))
	}
	if len(got.Spec.Egress) != 0 {
		t.Errorf("expected no egress rules, got %d", len(got.Spec.Egress))
	}
}

func TestNetworkPolicy_OperatorAllowSpec(t *testing.T) {
	r := newFakeNetworkPolicyReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))
	cluster := testCluster()

	np := networkPolicyManager{}
	if err := np.EnsureCreated(r, ctx, cluster); err != nil {
		t.Fatalf("EnsureCreated returned error: %v", err)
	}

	got := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: "lvms-operator-allow", Namespace: "default"}, got); err != nil {
		t.Fatalf("failed to get operator-allow NP: %v", err)
	}

	// podSelector should target lvms-operator
	if got.Spec.PodSelector.MatchLabels[constants.AppKubernetesNameLabel] != constants.ManagedByLabelVal {
		t.Errorf("expected podSelector to target %s, got %v", constants.ManagedByLabelVal, got.Spec.PodSelector.MatchLabels)
	}

	// Should have ingress rules (webhook 9443 + metrics 8443)
	if len(got.Spec.Ingress) != 2 {
		t.Errorf("expected 2 ingress rules, got %d", len(got.Spec.Ingress))
	}

	// Should have egress rules (API server + DNS)
	if len(got.Spec.Egress) != 2 {
		t.Errorf("expected 2 egress rules, got %d", len(got.Spec.Egress))
	}
}

func TestNetworkPolicy_VGManagerAllowSpec(t *testing.T) {
	r := newFakeNetworkPolicyReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))
	cluster := testCluster()

	np := networkPolicyManager{}
	if err := np.EnsureCreated(r, ctx, cluster); err != nil {
		t.Fatalf("EnsureCreated returned error: %v", err)
	}

	got := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: "lvms-vg-manager-allow", Namespace: "default"}, got); err != nil {
		t.Fatalf("failed to get vg-manager-allow NP: %v", err)
	}

	// podSelector should target vg-manager
	if got.Spec.PodSelector.MatchLabels[constants.AppKubernetesNameLabel] != constants.VGManagerLabelVal {
		t.Errorf("expected podSelector to target %s, got %v", constants.VGManagerLabelVal, got.Spec.PodSelector.MatchLabels)
	}

	// Should have 1 ingress rule (metrics 8443 only, no webhook)
	if len(got.Spec.Ingress) != 1 {
		t.Errorf("expected 1 ingress rule, got %d", len(got.Spec.Ingress))
	}

	// Should have egress rules (API server + DNS)
	if len(got.Spec.Egress) != 2 {
		t.Errorf("expected 2 egress rules, got %d", len(got.Spec.Egress))
	}
}

func TestNetworkPolicy_EnsureDeleted(t *testing.T) {
	r := newFakeNetworkPolicyReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))
	cluster := testCluster()

	np := networkPolicyManager{}
	// Create first
	if err := np.EnsureCreated(r, ctx, cluster); err != nil {
		t.Fatalf("EnsureCreated returned error: %v", err)
	}

	// Delete
	if err := np.EnsureDeleted(r, ctx, cluster); err != nil {
		t.Fatalf("EnsureDeleted returned error: %v", err)
	}

	// Verify all are gone
	for _, name := range []string{
		"lvms-operator-allow",
		"lvms-vg-manager-allow",
		"lvms-default-deny",
	} {
		got := &networkingv1.NetworkPolicy{}
		err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, got)
		if err == nil {
			t.Errorf("expected NetworkPolicy %s to be deleted", name)
		}
	}
}

func TestNetworkPolicy_EnsureDeleted_NotFound(t *testing.T) {
	r := newFakeNetworkPolicyReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))
	cluster := testCluster()

	np := networkPolicyManager{}
	// Should not error when NPs don't exist
	if err := np.EnsureDeleted(r, ctx, cluster); err != nil {
		t.Errorf("expected no error when NPs not found, got: %v", err)
	}
}

func TestNetworkPolicy_Idempotent(t *testing.T) {
	r := newFakeNetworkPolicyReconciler(t)
	ctx := log.IntoContext(context.Background(), testr.New(t))
	cluster := testCluster()

	np := networkPolicyManager{}
	// Create twice — should not error
	if err := np.EnsureCreated(r, ctx, cluster); err != nil {
		t.Fatalf("first EnsureCreated returned error: %v", err)
	}
	if err := np.EnsureCreated(r, ctx, cluster); err != nil {
		t.Fatalf("second EnsureCreated returned error: %v", err)
	}
}
