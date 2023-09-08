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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	logger := log.FromContext(ctx).WithValues("topolvmNode", c.getName())

	lvmVolumeGroups := lvmVolumeGroups(r.Namespace, lvmCluster.Spec.Storage.DeviceClasses)

	for _, volumeGroup := range lvmVolumeGroups {
		existingVolumeGroup := &lvmv1alpha1.LVMVolumeGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      volumeGroup.Name,
				Namespace: volumeGroup.Namespace,
			},
		}

		if err := cutil.SetControllerReference(lvmCluster, existingVolumeGroup, r.Scheme); err != nil {
			return fmt.Errorf("failed to set controller reference to LVMVolumeGroup: %w", err)
		}

		result, err := cutil.CreateOrUpdate(ctx, r.Client, existingVolumeGroup, func() error {
			existingVolumeGroup.Finalizers = volumeGroup.Finalizers
			existingVolumeGroup.Spec = volumeGroup.Spec
			return nil
		})

		if err != nil {
			return fmt.Errorf("%s failed to reconcile: %w", c.getName(), err)
		}

		logger.Info("LVMVolumeGroup applied to cluster", "operation", result, "name", volumeGroup.Name)
	}
	return nil
}

func (c lvmVG) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.getName())
	vgcrs := lvmVolumeGroups(r.Namespace, lvmCluster.Spec.Storage.DeviceClasses)

	var volumeGroupsPendingInStatus []string

	for _, volumeGroup := range vgcrs {
		vgName := client.ObjectKeyFromObject(volumeGroup)
		logger := logger.WithValues("LVMVolumeGroup", volumeGroup.GetName())

		if err := r.Client.Get(ctx, vgName, volumeGroup); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		// if not marked for deletion, mark now.
		// The controller reference will usually propagate all deletion timestamps so this will
		// only occur if the propagation from the API server was delayed.
		if volumeGroup.GetDeletionTimestamp().IsZero() {
			if err := r.Client.Delete(ctx, volumeGroup); err != nil {
				return fmt.Errorf("failed to delete LVMVolumeGroup %s: %w", volumeGroup.GetName(), err)
			}
		}

		// Has the VG been cleaned up on all hosts?
		if doesVGExistInDeviceClassStatus(volumeGroup.Name, lvmCluster) {
			volumeGroupsPendingInStatus = append(volumeGroupsPendingInStatus, vgName.String())
			continue
		}

		// Remove finalizer
		if update := cutil.RemoveFinalizer(volumeGroup, lvmvgFinalizer); update {
			if err := r.Client.Update(ctx, volumeGroup); err != nil {
				return fmt.Errorf("failed to remove finalizer from LVMVolumeGroup %s: %w", volumeGroup.GetName(), err)
			}
		}

		logger.Info("initiated LVMVolumeGroup deletion")
		volumeGroupsPendingInStatus = append(volumeGroupsPendingInStatus, vgName.String())
	}

	if len(volumeGroupsPendingInStatus) > 0 {
		return fmt.Errorf("waiting for LVMVolumeGroup's to be removed from nodestatus of %s: %v",
			client.ObjectKeyFromObject(lvmCluster), volumeGroupsPendingInStatus)
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
				Default:        len(deviceClasses) == 1 || deviceClass.Default, // True if there is only one device class or default is explicitly set.
			},
		}
		lvmVolumeGroups = append(lvmVolumeGroups, lvmVolumeGroup)
	}
	return lvmVolumeGroups
}

func doesVGExistInDeviceClassStatus(volumeGroup string, instance *lvmv1alpha1.LVMCluster) bool {
	dcStatuses := instance.Status.DeviceClassStatuses
	for _, dc := range dcStatuses {
		if dc.Name == volumeGroup {
			return true
		}
	}
	return false
}
