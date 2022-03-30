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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	lvmVGName = "lvmvg-manager"
)

type lvmVG struct{}

func (c lvmVG) getName() string {
	return lvmVGName
}

func (c lvmVG) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	lvmVolumeGroups := c.getLvmVolumeGroups(r, lvmCluster)

	for _, volumeGroup := range lvmVolumeGroups {
		result, err := cutil.CreateOrUpdate(ctx, r.Client, volumeGroup, func() error {
			// no need to mutate any field
			return nil
		})

		if err != nil {
			r.Log.Error(err, "failed to reconcile LVMVolumeGroup", "name", volumeGroup.Name)
			return err
		} else {
			r.Log.Info("successfully reconciled LVMVolumeGroup", "operation", result, "name", volumeGroup.Name)
		}

	}
	return nil
}

func (c lvmVG) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	existingLvmVg := &lvmv1alpha1.LVMVolumeGroup{}
	vgcrs := c.getLvmVolumeGroups(r, lvmCluster)

	for _, vgcr := range vgcrs {
		err := r.Client.Get(ctx, types.NamespacedName{Name: vgcr.Name, Namespace: vgcr.Namespace}, existingLvmVg)
		if err != nil {
			// already deleted in previous reconcile
			if errors.IsNotFound(err) {
				r.Log.Info("LVMVolumeGroup already deleted", "name", vgcr.Name)
				continue
			} else {
				r.Log.Error(err, "failed to retrieve LVMVolumeGroup", "name", vgcr.Name)
				return err
			}
		}

		// if not deleted, initiate deletion
		if existingLvmVg.GetDeletionTimestamp().IsZero() {
			if err = r.Client.Delete(ctx, existingLvmVg); err != nil {
				r.Log.Error(err, "failed to delete LVMVolumeGroup", "name", existingLvmVg.Name)
				return err
			} else {
				r.Log.Info("initiated LVMVolumeGroup deletion", "name", existingLvmVg.Name)
			}
		}
	}
	return nil
}

func (c lvmVG) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	// intentionally empty
	return nil
}

func (c lvmVG) getLvmVolumeGroups(r *LVMClusterReconciler, instance *lvmv1alpha1.LVMCluster) []*lvmv1alpha1.LVMVolumeGroup {

	lvmVolumeGroups := []*lvmv1alpha1.LVMVolumeGroup{}

	deviceClasses := instance.Spec.Storage.DeviceClasses
	for _, deviceClass := range deviceClasses {
		lvmVolumeGroup := &lvmv1alpha1.LVMVolumeGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deviceClass.Name,
				Namespace: r.Namespace,
			},
			Spec: lvmv1alpha1.LVMVolumeGroupSpec{
				NodeSelector:   deviceClass.NodeSelector,
				DeviceSelector: deviceClass.DeviceSelector,
			},
		}
		lvmVolumeGroups = append(lvmVolumeGroups, lvmVolumeGroup)
	}
	return lvmVolumeGroups
}
