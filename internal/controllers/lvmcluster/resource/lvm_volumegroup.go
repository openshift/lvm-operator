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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	lvmVGName = "lvmvg-manager"

	// legacyVGFinalizer is an old finalizer that was maintained before the Node specific finalizers in vgmanager
	// DEPRECATED
	// Use no finalizer and remove with 4.16
	legacyVGFinalizer = "lvm.openshift.io/lvmvolumegroup"
)

func LVMVGs() Manager {
	return lvmVG{}
}

type lvmVG struct{}

func (c lvmVG) GetName() string {
	return lvmVGName
}

func (c lvmVG) EnsureCreated(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("topolvmNode", c.GetName())

	lvmVolumeGroups := lvmVolumeGroups(r.GetNamespace(), lvmCluster.Spec.Storage.DeviceClasses)

	for _, volumeGroup := range lvmVolumeGroups {
		existingVolumeGroup := &lvmv1alpha1.LVMVolumeGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      volumeGroup.Name,
				Namespace: volumeGroup.Namespace,
			},
		}

		if err := cutil.SetControllerReference(lvmCluster, existingVolumeGroup, r.Scheme()); err != nil {
			return fmt.Errorf("failed to set controller reference to LVMVolumeGroup: %w", err)
		}

		result, err := cutil.CreateOrUpdate(ctx, r, existingVolumeGroup, func() error {
			// removes the old finalizer that was maintained up until 4.15, now we have vgmanager owned finalizers
			// per node starting with 4.15
			// This code path makes sure to remove the old finalizer if it is encountered from a previous installation
			if removed := cutil.RemoveFinalizer(existingVolumeGroup, legacyVGFinalizer); removed {
				logger.Info("removed legacy finalizer")
			}
			existingVolumeGroup.Spec = volumeGroup.Spec
			return nil
		})

		if err != nil {
			return fmt.Errorf("%s failed to reconcile: %w", c.GetName(), err)
		}

		if result != cutil.OperationResultNone {
			logger.V(2).Info("LVMVolumeGroup applied to cluster", "operation", result, "name", volumeGroup.Name)
		}
	}
	return nil
}

func (c lvmVG) EnsureDeleted(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName())
	vgcrs := lvmVolumeGroups(r.GetNamespace(), lvmCluster.Spec.Storage.DeviceClasses)

	var volumeGroupsPendingDelete []string

	for _, volumeGroup := range vgcrs {
		vgName := client.ObjectKeyFromObject(volumeGroup)
		logger := logger.WithValues("LVMVolumeGroup", volumeGroup.GetName())

		if err := r.Get(ctx, vgName, volumeGroup); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		// if not marked for deletion, mark now.
		// The controller reference will usually propagate all deletion timestamps so this will
		// only occur if the propagation from the API server was delayed.
		if volumeGroup.GetDeletionTimestamp().IsZero() {
			if err := r.Delete(ctx, volumeGroup); err != nil {
				return fmt.Errorf("failed to delete LVMVolumeGroup %s: %w", volumeGroup.GetName(), err)
			}
			logger.Info("initiated LVMVolumeGroup deletion", "volumeGroup", client.ObjectKeyFromObject(volumeGroup))
		} else {
			logger.Info("waiting for LVMVolumeGroup to be deleted", "volumeGroup", client.ObjectKeyFromObject(volumeGroup),
				"finalizers", volumeGroup.GetFinalizers())
		}

		volumeGroupsPendingDelete = append(volumeGroupsPendingDelete, vgName.String())
	}

	if len(volumeGroupsPendingDelete) > 0 {
		return fmt.Errorf("waiting for LVMVolumeGroup's to be removed of %s: %v",
			client.ObjectKeyFromObject(lvmCluster), volumeGroupsPendingDelete)
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
