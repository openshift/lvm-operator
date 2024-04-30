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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	lvmVGNodeStatusName = "lvmvgnodestatus"
)

func LVMVGNodeStatus() Manager {
	return lvmVGNodeStatus{}
}

type lvmVGNodeStatus struct{}

// lvmVGNodeStatus unit satisfies resourceManager interface
var _ Manager = lvmVGNodeStatus{}

func (l lvmVGNodeStatus) GetName() string {
	return lvmVGNodeStatusName
}

// EnsureCreated is a noop. This will be created by vg-manager.
func (l lvmVGNodeStatus) EnsureCreated(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	return nil
}

func (l lvmVGNodeStatus) EnsureDeleted(r Reconciler, ctx context.Context, _ *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", l.GetName())
	vgNodeStatusList := &lvmv1alpha1.LVMVolumeGroupNodeStatusList{}
	if err := r.List(ctx, vgNodeStatusList, client.InNamespace(r.GetNamespace())); err != nil {
		return fmt.Errorf("failed to list LVMVolumeGroupNodeStatus: %w", err)
	}

	var volumeGroupNodeStatusesPendingDelete []string
	for _, nodeItem := range vgNodeStatusList.Items {
		var volumeGroupsPendingDelete []string
		for _, item := range nodeItem.Spec.LVMVGStatus {
			lvmVolumeGroup := &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      item.Name,
					Namespace: r.GetNamespace(),
				},
			}

			if err := r.Get(ctx, client.ObjectKeyFromObject(lvmVolumeGroup), lvmVolumeGroup); err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return err
			}

			logger.Info("waiting for LVMVolumeGroup to be deleted", "volumeGroup", client.ObjectKeyFromObject(lvmVolumeGroup),
				"finalizers", lvmVolumeGroup.GetFinalizers(), "LVMVolumeGroupNodeStatusName", nodeItem.GetName())

			volumeGroupsPendingDelete = append(volumeGroupsPendingDelete, lvmVolumeGroup.GetName())
		}

		if len(volumeGroupsPendingDelete) > 0 {
			volumeGroupNodeStatusesPendingDelete = append(volumeGroupNodeStatusesPendingDelete, nodeItem.GetName())
			continue
		}

		if !nodeItem.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the LVMVolumeGroupNodeStatus %s is still present, waiting for deletion", nodeItem.GetName())
		}
		if err := r.Delete(ctx, &nodeItem); err != nil {
			return fmt.Errorf("failed to delete LVMVolumeGroupNodeStatus %s: %w", nodeItem.GetName(), err)
		}

		logger.Info("initiated LVMVolumeGroupNodeStatus deletion", "LVMVolumeGroupNodeStatusName", nodeItem.GetName())
	}

	if len(volumeGroupNodeStatusesPendingDelete) > 0 {
		return fmt.Errorf("waiting for LVMVolumeGroups to be removed before removing LVMVolumeGroupNodeStatuses: %v", volumeGroupNodeStatusesPendingDelete)
	}

	return nil
}
