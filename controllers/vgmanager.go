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

package controllers

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type vgManager struct{ *runtime.Scheme }

var _ resourceManager = vgManager{}

const (
	VGManagerUnit = "vg-manager"
)

func (v vgManager) getName() string {
	return VGManagerUnit
}

func (v vgManager) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	unitLogger := r.Log.WithValues("resourceManager", v.getName())

	// get desired daemonset spec
	ds := newVGManagerDaemonset(lvmCluster, r.Namespace, r.ImageName)
	if versioned, err := v.Scheme.ConvertToVersion(ds,
		runtime.GroupVersioner(schema.GroupVersions(v.Scheme.PrioritizedVersionsAllGroups()))); err == nil {
		ds = versioned.(*appsv1.DaemonSet)
	}

	// controller reference
	err := ctrl.SetControllerReference(lvmCluster, ds, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to set controller reference on vgManager daemonset %q. %v", ds.Name, err)
	}

	unitLogger.Info("Apply")
	// the anonymous mutate function modifies the daemonset object after fetching it.
	// if the daemonset does not already exist, it creates it, otherwise, it updates it

	err = r.Client.Patch(ctx, ds, client.Apply, client.ForceOwnership, client.FieldOwner(ControllerName))
	if err != nil {
		r.Log.Error(err, "failed to create or update vgManager daemonset", "name", ds.Name)
		return err
	}
	return err
}

// ensureDeleted is a noop. Deletion will be handled by ownerref
func (v vgManager) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	return nil
}

func initMapIfNil(m *map[string]string) {
	if len(*m) > 1 {
		return
	}
	*m = make(map[string]string)
}
