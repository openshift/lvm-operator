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

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type clusterObjects struct {
	namespaces     []k8sv1.Namespace
	operatorGroups []v1.OperatorGroup
	catalogSources []v1alpha1.CatalogSource
	subscriptions  []v1alpha1.Subscription
}

// generateClusterObjects generates the cluster objects required for deploying the operator using OLM.
func generateClusterObjects(lvmCatalogImage string, subscriptionChannel string) *clusterObjects {
	co := &clusterObjects{}
	label := make(map[string]string)
	// label required for monitoring this namespace
	label["openshift.io/cluster-monitoring"] = "true"

	annotations := make(map[string]string)
	// annotation required for workload partitioning
	annotations["workload.openshift.io/allowed"] = "management"

	// Namespaces
	co.namespaces = append(co.namespaces, k8sv1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        installNamespace,
			Annotations: annotations,
			Labels:      label,
		},
	})

	// Operator Groups
	lvmOG := v1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openshift-storage-operatorgroup",
			Namespace: installNamespace,
		},
		Spec: v1.OperatorGroupSpec{
			TargetNamespaces: []string{installNamespace},
		},
	}
	lvmOG.SetGroupVersionKind(schema.GroupVersionKind{Group: v1.SchemeGroupVersion.Group, Kind: "OperatorGroup", Version: v1.SchemeGroupVersion.Version})

	co.operatorGroups = append(co.operatorGroups, lvmOG)

	// Catalog Source
	lvmCatalog := v1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvms-catalogsource",
			Namespace: installNamespace,
		},
		Spec: v1alpha1.CatalogSourceSpec{
			SourceType:  v1alpha1.SourceTypeGrpc,
			Image:       lvmCatalogImage,
			DisplayName: "Red Hat, Inc.",
			Publisher:   "Red Hat",
		},
	}
	lvmCatalog.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "CatalogSource", Version: v1alpha1.SchemeGroupVersion.Version})

	co.catalogSources = append(co.catalogSources, lvmCatalog)

	// Subscriptions
	lvmSubscription := v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvms-operator",
			Namespace: installNamespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                subscriptionChannel,
			InstallPlanApproval:    "Automatic",
			CatalogSource:          "lvms-catalogsource",
			CatalogSourceNamespace: installNamespace,
			Package:                "lvms-operator",
		},
	}
	lvmSubscription.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "Subscription", Version: v1alpha1.SchemeGroupVersion.Version})
	co.subscriptions = append(co.subscriptions, lvmSubscription)

	return co
}

// waitForLVMCatalogSource waits for LVM catalog source.
func waitForLVMCatalogSource(ctx context.Context) bool {
	labelSelector, err := labels.Parse("olm.catalogSource in (lvms-catalogsource)")
	Expect(err).ToNot(HaveOccurred())

	return Eventually(func(g Gomega, ctx context.Context) {
		pods := &k8sv1.PodList{}
		g.Expect(crClient.List(ctx, pods, &crclient.ListOptions{
			LabelSelector: labelSelector,
			Namespace:     installNamespace,
		})).To(Succeed())

		g.Expect(pods.Items).ToNot(BeEmpty(), "waiting on lvms catalog source pod to be created")

		isReady := false
		for _, pod := range pods.Items {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == k8sv1.PodReady && condition.Status == k8sv1.ConditionTrue {
					isReady = true
					break
				}
			}
		}
		g.Expect(isReady).To(BeTrue())

	}).WithTimeout(300 * time.Second).WithPolling(10 * time.Second).WithContext(ctx).Should(Succeed())
}

// waitForLVMOperator waits for the lvm-operator to come online.
func waitForLVMOperator(ctx context.Context) bool {
	deployments := []string{"lvms-operator"}

	return Eventually(func(g Gomega, ctx context.Context) {
		for _, name := range deployments {
			deployment := &appsv1.Deployment{}
			g.Expect(crClient.Get(ctx, types.NamespacedName{Name: name, Namespace: installNamespace}, deployment)).To(Succeed())

			isAvailable := false
			for _, condition := range deployment.Status.Conditions {
				if condition.Type == appsv1.DeploymentAvailable && condition.Status == k8sv1.ConditionTrue {
					isAvailable = true
					break
				}
			}

			g.Expect(isAvailable).To(BeTrue())
		}
	}).WithTimeout(1000 * time.Second).WithPolling(10 * time.Second).WithContext(ctx).Should(Succeed())
}

// deployClusterObjects deploys the cluster objects.
func deployClusterObjects(ctx context.Context, co *clusterObjects) {

	for _, namespace := range co.namespaces {
		createNamespace(ctx, namespace.Name)
	}

	for _, operatorGroup := range co.operatorGroups {
		operatorGroup := operatorGroup
		operatorGroups := &v1.OperatorGroupList{}
		Expect(crClient.List(ctx, operatorGroups, &crclient.ListOptions{
			Namespace: installNamespace,
		})).To(Succeed())

		// There should be only one operatorgroup in a namespace.
		// The system is already misconfigured - error out.
		Expect(operatorGroups.Items).To(HaveLen(1), "more than one operatorgroup per namespace is not allowed")

		if len(operatorGroups.Items) > 0 {
			// There should be only one operatorgroup in a namespace.
			// Skip this one, so we don't make the system bad.
			continue
		}
		err := crClient.Create(ctx, &operatorGroup)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	for _, catalogSource := range co.catalogSources {
		catalogSource := catalogSource
		err := crClient.Create(ctx, &catalogSource)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	// Wait for catalog source before posting subscription
	waitForLVMCatalogSource(ctx)

	for _, subscription := range co.subscriptions {
		subscription := subscription
		err := crClient.Create(ctx, &subscription)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	// Wait on lvm-operator to come online.
	waitForLVMOperator(ctx)
}

// deployLVMWithOLM deploys lvm operator via an olm subscription.
func deployLVMWithOLM(ctx context.Context, lvmCatalogImage string, subscriptionChannel string) {
	Expect(lvmCatalogImage).ToNot(BeEmpty(), "catalog registry images must be supplied")
	co := generateClusterObjects(lvmCatalogImage, subscriptionChannel)
	deployClusterObjects(ctx, co)
}

// uninstallLVM uninstalls lvm operator.
func uninstallLVM(ctx context.Context, lvmCatalogImage string, subscriptionChannel string) {
	GinkgoHelper()

	// Delete remaining operator manifests
	co := generateClusterObjects(lvmCatalogImage, subscriptionChannel)

	for _, operatorGroup := range co.operatorGroups {
		DeleteResource(ctx, &operatorGroup)
	}

	for _, catalogSource := range co.catalogSources {
		DeleteResource(ctx, &catalogSource)
	}

	for _, subscription := range co.subscriptions {
		DeleteResource(ctx, &subscription)
	}

	for _, namespace := range co.namespaces {
		DeleteResource(ctx, &namespace)
	}
}
