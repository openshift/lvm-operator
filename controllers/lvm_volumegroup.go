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
	"errors"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	k8serror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	lvmVGName      = "lvmvg-manager"
	lvmvgFinalizer = "lvm.openshift.io/lvmvolumegroup"
)

type lvmVG struct{}

func (c lvmVG) getName() string {
	return lvmVGName
}

func (c lvmVG) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	lvmVolumeGroups := lvmVolumeGroups(r.Namespace, lvmCluster.Spec.Storage.DeviceClasses)

	for _, volumeGroup := range lvmVolumeGroups {
		existingVolumeGroup := &lvmv1alpha1.LVMVolumeGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      volumeGroup.Name,
				Namespace: volumeGroup.Namespace,
			},
		}
		result, err := cutil.CreateOrUpdate(ctx, r.Client, existingVolumeGroup, func() error {
			existingVolumeGroup.Finalizers = volumeGroup.Finalizers
			existingVolumeGroup.Spec = volumeGroup.Spec
			return nil
		})

		if err != nil {
			r.Log.Error(err, "failed to reconcile LVMVolumeGroup", "name", volumeGroup.Name)
			return err
		}
		r.Log.Info("successfully reconciled LVMVolumeGroup", "operation", result, "name", volumeGroup.Name)
	}
	return nil
}

func (c lvmVG) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	vgcrs := lvmVolumeGroups(r.Namespace, lvmCluster.Spec.Storage.DeviceClasses)
	allVGsDeleted := true

	for _, vgcr := range vgcrs {
		currentLvmVg := &lvmv1alpha1.LVMVolumeGroup{}

		err := r.Client.Get(ctx, types.NamespacedName{Name: vgcr.Name, Namespace: vgcr.Namespace}, currentLvmVg)
		if err != nil {
			// already deleted in previous reconcile
			if k8serror.IsNotFound(err) {
				r.Log.Info("LVMVolumeGroup already deleted", "name", vgcr.Name)
				continue
			}
			r.Log.Error(err, "failed to retrieve LVMVolumeGroup", "name", vgcr.Name)
			return err
		}

		// if not deleted, initiate deletion
		if currentLvmVg.GetDeletionTimestamp().IsZero() {
			err = r.Client.Delete(ctx, currentLvmVg)
			if err != nil {
				r.Log.Error(err, "failed to delete LVMVolumeGroup", "name", currentLvmVg.Name)
				return err
			}
			r.Log.Info("initiated LVMVolumeGroup deletion", "name", currentLvmVg.Name)
			allVGsDeleted = false
		} else {
			// Has the VG been cleaned up on all hosts?
			exists := doesVGExistOnHosts(currentLvmVg.Name, lvmCluster)
			if !exists {
				// Remove finalizer
				cutil.RemoveFinalizer(currentLvmVg, lvmvgFinalizer)
				err = r.Client.Update(ctx, currentLvmVg)
				if err != nil {
					return err
				}
			} else {
				allVGsDeleted = false
			}
		}
	}

	if !allVGsDeleted {
		return errors.New("waiting for all VGs to be deleted")
	}

	return nil
}

func lvmVolumeGroups(namespace string, deviceClasses []lvmv1alpha1.DeviceClass) []*lvmv1alpha1.LVMVolumeGroup {

	lvmVolumeGroups := make([]*lvmv1alpha1.LVMVolumeGroup, 0, len(deviceClasses))

	for _, deviceClass := range deviceClasses {
		lvmVolumeGroup := &lvmv1alpha1.LVMVolumeGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deviceClass.Name,
				Namespace: namespace,
				Finalizers: []string{
					lvmvgFinalizer,
				},
			},
			Spec: lvmv1alpha1.LVMVolumeGroupSpec{
				NodeSelector:   deviceClass.NodeSelector,
				DeviceSelector: deviceClass.DeviceSelector,
				ThinPoolConfig: deviceClass.ThinPoolConfig,
				Default:        len(deviceClasses) == 1 || deviceClass.Default, //True if there is only one device class or default is explicitly set.
			},
		}
		lvmVolumeGroups = append(lvmVolumeGroups, lvmVolumeGroup)
	}
	return lvmVolumeGroups
}

func doesVGExistOnHosts(volumeGroup string, instance *lvmv1alpha1.LVMCluster) bool {

	dcStatuses := instance.Status.DeviceClassStatuses
	for _, dc := range dcStatuses {
		if dc.Name == volumeGroup {
			return true
		}
	}
	return false
}
