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
	"fmt"
	"time"

	v1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
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
func waitForLVMCatalogSource(ctx context.Context) error {
	timeout := 300 * time.Second
	interval := 10 * time.Second
	lastReason := ""

	labelSelector, err := labels.Parse("olm.catalogSource in (lvms-catalogsource)")
	if err != nil {
		return err
	}
	err = utilwait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (done bool, err error) {

		pods := &k8sv1.PodList{}
		err = crClient.List(ctx, pods, &crclient.ListOptions{
			LabelSelector: labelSelector,
			Namespace:     installNamespace,
		})
		if err != nil {
			lastReason = fmt.Sprintf("error talking to k8s apiserver: %v", err)
			return false, nil
		}

		if len(pods.Items) == 0 {
			lastReason = "waiting on lvms catalog source pod to be created"
			return false, nil
		}
		isReady := false
		for _, pod := range pods.Items {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == k8sv1.PodReady && condition.Status == k8sv1.ConditionTrue {
					isReady = true
					break
				}
			}
		}
		if !isReady {
			lastReason = "waiting on lvms catalog source pod to reach ready state"
			return false, nil
		}

		// if we get here, then all deployments are created and available
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}

// waitForLVMOperator waits for the lvm-operator to come online.
func waitForLVMOperator(ctx context.Context) error {
	deployments := []string{"lvms-operator"}

	timeout := 1000 * time.Second
	interval := 10 * time.Second

	lastReason := ""

	err := utilwait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (done bool, err error) {
		for _, name := range deployments {
			deployment := &appsv1.Deployment{}
			err = crClient.Get(ctx, types.NamespacedName{Name: name, Namespace: installNamespace}, deployment)
			if err != nil {
				lastReason = fmt.Sprintf("waiting on deployment %s to be created", name)
				return false, nil
			}

			isAvailable := false
			for _, condition := range deployment.Status.Conditions {
				if condition.Type == appsv1.DeploymentAvailable && condition.Status == k8sv1.ConditionTrue {
					isAvailable = true
					break
				}
			}

			if !isAvailable {
				lastReason = fmt.Sprintf("waiting on deployment %s to become available", name)
				return false, nil
			}
		}

		// if we get here, then all deployments are created and available
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}

// deployClusterObjects deploys the cluster objects.
func deployClusterObjects(ctx context.Context, co *clusterObjects) error {

	for _, namespace := range co.namespaces {
		err := createNamespace(ctx, namespace.Name)
		if err != nil {
			return err
		}
	}

	for _, operatorGroup := range co.operatorGroups {
		operatorGroup := operatorGroup
		operatorGroups := &v1.OperatorGroupList{}
		err := crClient.List(ctx, operatorGroups, &crclient.ListOptions{
			Namespace: installNamespace,
		})
		if err != nil {
			return err
		}
		if len(operatorGroups.Items) > 1 {
			// There should be only one operatorgroup in a namespace.
			// The system is already misconfigured - error out.
			return fmt.Errorf("more than one operatorgroup detected in namespace %v - aborting", operatorGroup.Namespace)
		}
		if len(operatorGroups.Items) > 0 {
			// There should be only one operatorgroup in a namespace.
			// Skip this one, so we don't make the system bad.
			continue
		}
		err = crClient.Create(ctx, &operatorGroup)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	for _, catalogSource := range co.catalogSources {
		catalogSource := catalogSource
		err := crClient.Create(ctx, &catalogSource)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	// Wait for catalog source before posting subscription
	err := waitForLVMCatalogSource(ctx)
	if err != nil {
		return err
	}

	for _, subscription := range co.subscriptions {
		subscription := subscription
		err := crClient.Create(ctx, &subscription)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	// Wait on lvm-operator to come online.
	err = waitForLVMOperator(ctx)
	if err != nil {
		return err
	}

	return nil
}

// deployLVMWithOLM deploys lvm operator via an olm subscription.
func deployLVMWithOLM(ctx context.Context, lvmCatalogImage string, subscriptionChannel string) error {

	if lvmCatalogImage == "" {
		return fmt.Errorf("catalog registry images not supplied")
	}

	co := generateClusterObjects(lvmCatalogImage, subscriptionChannel)
	err := deployClusterObjects(ctx, co)
	if err != nil {
		return err
	}

	return nil
}

// deleteClusterObjects deletes remaining operator manifests.
func deleteClusterObjects(ctx context.Context, co *clusterObjects) error {

	for _, operatorGroup := range co.operatorGroups {
		operatorgroup := operatorGroup
		err := crClient.Delete(ctx, &operatorgroup)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

	}

	for _, catalogSource := range co.catalogSources {
		catalogsource := catalogSource
		err := crClient.Delete(ctx, &catalogsource)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	for _, subscription := range co.subscriptions {
		subs := subscription
		err := crClient.Delete(ctx, &subs)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// uninstallLVM uninstalls lvm operator.
func uninstallLVM(ctx context.Context, lvmCatalogImage string, subscriptionChannel string) error {
	// Delete remaining operator manifests
	co := generateClusterObjects(lvmCatalogImage, subscriptionChannel)
	err := deleteClusterObjects(ctx, co)
	if err != nil {
		return err
	}
	for _, namespace := range co.namespaces {
		err = deleteNamespaceAndWait(ctx, namespace.Name)
		if err != nil {
			return err
		}
	}

	return nil
}
