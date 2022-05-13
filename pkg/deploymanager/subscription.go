package deploymanager

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/operator-framework/api/pkg/operators/v1"
	v1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type clusterObjects struct {
	namespaces     []k8sv1.Namespace
	operatorGroups []v1.OperatorGroup
	catalogSources []v1alpha1.CatalogSource
	subscriptions  []v1alpha1.Subscription
}

// Generating the cluster objects.
func (t *DeployManager) generateClusterObjects(lvmCatalogImage string, subscriptionChannel string) *clusterObjects {

	co := &clusterObjects{}
	label := make(map[string]string)
	// Label required for monitoring this namespace
	label["openshift.io/cluster-monitoring"] = "true"

	// Namespaces
	co.namespaces = append(co.namespaces, k8sv1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   InstallNamespace,
			Labels: label,
		},
	})

	// Operator Groups
	lvmOG := v1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openshift-storage-operatorgroup",
			Namespace: InstallNamespace,
		},
		Spec: v1.OperatorGroupSpec{
			TargetNamespaces: []string{InstallNamespace},
		},
	}
	lvmOG.SetGroupVersionKind(schema.GroupVersionKind{Group: v1.SchemeGroupVersion.Group, Kind: "OperatorGroup", Version: v1.SchemeGroupVersion.Version})

	co.operatorGroups = append(co.operatorGroups, lvmOG)

	// Catalog Source
	lvmCatalog := v1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvm-catalogsource",
			Namespace: InstallNamespace,
		},
		Spec: v1alpha1.CatalogSourceSpec{
			SourceType:  v1alpha1.SourceTypeGrpc,
			Image:       lvmCatalogImage,
			DisplayName: "OpenShift Data Foundation",
			Publisher:   "Red Hat",
		},
	}
	lvmCatalog.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "CatalogSource", Version: v1alpha1.SchemeGroupVersion.Version})

	co.catalogSources = append(co.catalogSources, lvmCatalog)

	// Subscriptions
	lvmSubscription := v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvm-operator",
			Namespace: InstallNamespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                subscriptionChannel,
			InstallPlanApproval:    "Automatic",
			CatalogSource:          "lvm-catalogsource",
			CatalogSourceNamespace: InstallNamespace,
			// Upstream package name is lvm-operator (downstream will be odf-lvm-operator)
			Package: "lvm-operator",
		},
	}
	lvmSubscription.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "Subscription", Version: v1alpha1.SchemeGroupVersion.Version})
	co.subscriptions = append(co.subscriptions, lvmSubscription)

	return co
}

// Waiting for LVM catalog source.
func (t *DeployManager) waitForLVMCatalogSource() error {
	timeout := 300 * time.Second
	interval := 10 * time.Second

	lastReason := ""

	labelSelector, err := labels.Parse("olm.catalogSource in (lvm-catalogsource)")
	if err != nil {
		return err
	}
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {

		pods := &k8sv1.PodList{}
		err = t.crClient.List(context.TODO(), pods, &crClient.ListOptions{
			LabelSelector: labelSelector,
			Namespace:     InstallNamespace,
		})
		if err != nil {
			lastReason = fmt.Sprintf("error talking to k8s apiserver: %v", err)
			return false, nil
		}

		if len(pods.Items) == 0 {
			lastReason = "waiting on lvm catalog source pod to be created"
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
			lastReason = "waiting on lvm catalog source pod to reach ready state"
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

// WaitForLVMOperator waits for the lvm-operator to come online.
func (t *DeployManager) waitForLVMOperator() error {
	deployments := []string{"lvm-operator-controller-manager"}

	timeout := 1000 * time.Second
	interval := 10 * time.Second

	lastReason := ""

	err := utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		for _, name := range deployments {
			deployment := &appsv1.Deployment{}
			err = t.crClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: InstallNamespace}, deployment)
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

// Deploying the cluster objects.
func (t *DeployManager) deployClusterObjects(co *clusterObjects) error {

	for _, namespace := range co.namespaces {
		err := t.CreateNamespace(namespace.Name)
		if err != nil {
			return err
		}
	}

	for _, operatorGroup := range co.operatorGroups {
		operatorGroup := operatorGroup
		operatorGroups := &v1.OperatorGroupList{}
		err := t.crClient.List(context.TODO(), operatorGroups, &crClient.ListOptions{
			Namespace: InstallNamespace,
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
		err = t.crClient.Create(context.TODO(), &operatorGroup)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	for _, catalogSource := range co.catalogSources {
		catalogSource := catalogSource
		err := t.crClient.Create(context.TODO(), &catalogSource)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	// Wait for catalog source before posting subscription
	err := t.waitForLVMCatalogSource()
	if err != nil {
		return err
	}

	for _, subscription := range co.subscriptions {
		subscription := subscription
		err := t.crClient.Create(context.TODO(), &subscription)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	// Wait on lvm-operator to come online.
	err = t.waitForLVMOperator()
	if err != nil {
		return err
	}

	return nil
}

// DeployLVMWithOLM deploys lvm operator via an olm subscription.
func (t *DeployManager) DeployLVMWithOLM(lvmCatalogImage string, subscriptionChannel string) error {

	if lvmCatalogImage == "" {
		return fmt.Errorf("catalog registry images not supplied")
	}

	co := t.generateClusterObjects(lvmCatalogImage, subscriptionChannel)
	err := t.deployClusterObjects(co)
	if err != nil {
		return err
	}

	return nil
}

// DeleteClusterObjects deletes remaining operator manifests.
func (t *DeployManager) deleteClusterObjects(co *clusterObjects) error {

	for _, operatorGroup := range co.operatorGroups {
		operatorgroup := operatorGroup
		err := t.crClient.Delete(context.TODO(), &operatorgroup)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

	}

	for _, catalogSource := range co.catalogSources {
		catalogsource := catalogSource
		err := t.crClient.Delete(context.TODO(), &catalogsource)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	for _, subscription := range co.subscriptions {
		subs := subscription
		err := t.crClient.Delete(context.TODO(), &subs)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// UninstallLVM uninstalls lvm operator.
func (t *DeployManager) UninstallLVM(lvmCatalogImage string, subscriptionChannel string) error {
	// Delete remaining operator manifests
	co := t.generateClusterObjects(lvmCatalogImage, subscriptionChannel)
	err := t.deleteClusterObjects(co)
	if err != nil {
		return err
	}
	for _, namespace := range co.namespaces {
		err = t.DeleteNamespaceAndWait(namespace.Name)
		if err != nil {
			return err
		}
	}

	return nil
}
