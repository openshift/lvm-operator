/*
Copyright 2021.

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

package controllers

import (
	"context"

	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type vgManager struct{}

var _ resourceManager = vgManager{}

const (
	VGManagerUnit = "vg-manager"
)

func (v vgManager) getName() string {
	return VGManagerUnit
}

func (v vgManager) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	unitLogger := r.Log.WithValues("resourceManager", v.getName())
	// aggregate nodeSelector and tolerations from all deviceClasses
	nodeSelector, tolerations := extractNodeSelectorAndTolerations(*lvmCluster)
	// get desired daemonset spec
	dsTemplate := newVGManagerDaemonset(*lvmCluster)
	// create desired daemonset or update mutable fields on existing one
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dsTemplate.Name,
			Namespace: dsTemplate.Namespace,
		},
	}
	unitLogger.Info("running CreateOrUpdate")
	// the anonymous mutate function modifies the daemonset object after fetching it.
	// if the daemonset does not already exist, it creates it, otherwise, it updates it
	result, err := ctrl.CreateOrUpdate(ctx, r.Client, ds, func() error {
		// at creation, deep copy the whole daemonset
		if ds.CreationTimestamp.IsZero() {
			dsTemplate.DeepCopyInto(ds)
			return nil
		}
		// if update, update only mutable fields

		// copy selector labels to daemonset and template
		initMapIfNil(&ds.ObjectMeta.Labels)
		initMapIfNil(&ds.Spec.Template.ObjectMeta.Labels)
		for key, value := range dsTemplate.Labels {
			ds.ObjectMeta.Labels[key] = value
			ds.Spec.Template.ObjectMeta.Labels[key] = value
		}

		// containers
		ds.Spec.Template.Spec.Containers = dsTemplate.Spec.Template.Spec.Containers
		if r.DevelopmentMode {
			unitLogger.Info("configuring vgmanager with development mode")
			ds.Spec.Template.Spec.Containers[0].Args = append(ds.Spec.Template.Spec.Containers[0].Args, "-development")
		}

		// volumes
		ds.Spec.Template.Spec.Volumes = dsTemplate.Spec.Template.Spec.Volumes

		// service account
		// TODO(rohan) get from env
		ds.Spec.Template.Spec.ServiceAccountName = "lvm-operator-vg-manager"

		// controller reference
		err := ctrl.SetControllerReference(lvmCluster, ds, r.Scheme)
		if err != nil {
			return err
		}
		// tolerations
		ds.Spec.Template.Spec.Tolerations = tolerations

		// nodeSelector if non-nil
		if nodeSelector != nil {
			ds.Spec.Template.Spec.Affinity = &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
				},
			}
		} else {
			ds.Spec.Template.Spec.Affinity = nil
		}
		return nil
	})
	if err != nil {
		return err
	} else if result != controllerutil.OperationResultNone {
		unitLogger.Info("daemonset modified", "operation", result, "name", ds.Name)
	} else {
		unitLogger.Info("daemonset unchanged")
	}
	return err
}

// extractNodeSelectorAndTolerations combines and extracts scheduling parameters from the multiple deviceClass entries in an lvmCluster
func extractNodeSelectorAndTolerations(lvmCluster lvmv1alpha1.LVMCluster) (*corev1.NodeSelector, []corev1.Toleration) {
	var nodeSelector *corev1.NodeSelector
	var tolerations []corev1.Toleration
	terms := make([]corev1.NodeSelectorTerm, 0)
	matchAllNodes := false
	for _, deviceClass := range lvmCluster.Spec.DeviceClasses {
		tolerations = append(tolerations, deviceClass.Tolerations...)
		if deviceClass.NodeSelector != nil {
			terms = append(terms, deviceClass.NodeSelector.NodeSelectorTerms...)
		} else {
			matchAllNodes = true
		}
	}
	// populate a nodeSelector unless one or more of the deviceClasses match all nodes with a nil nodeSelector
	if !matchAllNodes {
		nodeSelector = &corev1.NodeSelector{NodeSelectorTerms: terms}
	}
	return nodeSelector, tolerations
}

// noop, handled by ownerref
func (v vgManager) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	return nil
}

// TODO
func (v vgManager) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	return nil
}
func initMapIfNil(m *map[string]string) {
	if len(*m) > 1 {
		return
	}
	*m = make(map[string]string)
}
