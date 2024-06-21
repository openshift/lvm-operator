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
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func VGManager() Manager {
	return vgManager{}
}

type vgManager struct{}

var _ Manager = vgManager{}

const (
	VGManagerUnit = "vg-manager"
)

func (v vgManager) GetName() string {
	return VGManagerUnit
}

func (v vgManager) EnsureCreated(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", v.GetName())

	// get desired daemonset spec
	dsTemplate := newVGManagerDaemonset(
		lvmCluster,
		r.GetNamespace(),
		r.GetImageName(),
		r.GetVGManagerCommand(),
		r.GetLogPassthroughOptions().VGManager.AsArgs(),
	)
	if err := ctrl.SetControllerReference(lvmCluster, &dsTemplate, r.Scheme()); err != nil {
		return fmt.Errorf("failed to set controller reference on vgManager daemonset %q. %v", dsTemplate.Name, err)
	}

	// create desired daemonset or update mutable fields on existing one
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dsTemplate.Name,
			Namespace: dsTemplate.Namespace,
		},
	}
	// the anonymous mutate function modifies the daemonset object after fetching it.
	// if the daemonset does not already exist, it creates it, otherwise, it updates it
	result, err := ctrl.CreateOrUpdate(ctx, r, ds, func() error {
		// at creation, deep copy the whole daemonset
		if ds.CreationTimestamp.IsZero() {
			dsTemplate.DeepCopyInto(ds)
			return nil
		}

		if err := ctrl.SetControllerReference(lvmCluster, ds, r.Scheme()); err != nil {
			return fmt.Errorf("failed to set controller reference on vgManager daemonset %q. %v", dsTemplate.Name, err)
		}

		// if update, update only mutable fields
		initMapIfNil(&ds.ObjectMeta.Labels)
		initMapIfNil(&ds.Spec.Template.ObjectMeta.Labels)
		for key, value := range dsTemplate.Labels {
			ds.ObjectMeta.Labels[key] = value
			ds.Spec.Template.ObjectMeta.Labels[key] = value
		}

		initMapIfNil(&ds.ObjectMeta.Annotations)
		for key, value := range dsTemplate.Annotations {
			ds.ObjectMeta.Annotations[key] = value
		}

		initMapIfNil(&ds.Spec.Template.Annotations)
		for key, value := range dsTemplate.Spec.Template.Annotations {
			ds.Spec.Template.Annotations[key] = value
		}

		ds.Spec.Template.Spec.Containers = dsTemplate.Spec.Template.Spec.Containers
		ds.Spec.Template.Spec.Volumes = dsTemplate.Spec.Template.Spec.Volumes
		ds.Spec.Template.Spec.ServiceAccountName = dsTemplate.Spec.Template.Spec.ServiceAccountName
		ds.Spec.Template.Spec.PriorityClassName = dsTemplate.Spec.Template.Spec.PriorityClassName
		ds.Spec.Template.Spec.Tolerations = dsTemplate.Spec.Template.Spec.Tolerations

		if dsTemplate.Spec.Template.Spec.Affinity != nil {
			setDaemonsetNodeSelector(dsTemplate.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution, ds)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("%s failed to reconcile: %w", v.GetName(), err)
	}

	if result != cutil.OperationResultNone {
		logger.V(2).Info("DaemonSet applied to cluster", "operation", result, "name", ds.Name)
	}

	if err := VerifyDaemonSetReadiness(ds); err != nil {
		return fmt.Errorf("DaemonSet is not considered ready: %w", err)
	}

	return nil
}

// ensureDeleted is a noop. Deletion will be handled by ownerref
func (v vgManager) EnsureDeleted(_ Reconciler, _ context.Context, _ *lvmv1alpha1.LVMCluster) error {
	return nil
}

func initMapIfNil(m *map[string]string) {
	if len(*m) > 1 {
		return
	}
	*m = make(map[string]string)
}
