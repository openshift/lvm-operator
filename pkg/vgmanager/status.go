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

package vgmanager

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/pkg/lvm"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *VGReconciler) setVolumeGroupProgressingStatus(ctx context.Context, vg *lvmv1alpha1.LVMVolumeGroup) error {
	status := &lvmv1alpha1.VGStatus{
		Name:   vg.GetName(),
		Status: lvmv1alpha1.VGStatusProgressing,
	}

	// Set devices for the VGStatus.
	if _, err := r.setDevices(status); err != nil {
		return err
	}

	return r.setVolumeGroupStatus(ctx, vg, status)
}

func (r *VGReconciler) setVolumeGroupReadyStatus(ctx context.Context, vg *lvmv1alpha1.LVMVolumeGroup) error {
	status := &lvmv1alpha1.VGStatus{
		Name:   vg.GetName(),
		Status: lvmv1alpha1.VGStatusReady,
	}

	// Set devices for the VGStatus.
	if _, err := r.setDevices(status); err != nil {
		return err
	}

	return r.setVolumeGroupStatus(ctx, vg, status)
}

func (r *VGReconciler) setVolumeGroupFailedStatus(ctx context.Context, vg *lvmv1alpha1.LVMVolumeGroup, err error) error {
	status := &lvmv1alpha1.VGStatus{
		Name:   vg.GetName(),
		Status: lvmv1alpha1.VGStatusFailed,
		Reason: err.Error(),
	}

	// Set devices for the VGStatus.
	// If there is backing volume group, then set as degraded
	if devicesExist, err := r.setDevices(status); err != nil {
		return fmt.Errorf("could not set devices in VGStatus: %w", err)
	} else if devicesExist {
		status.Status = lvmv1alpha1.VGStatusDegraded
	}

	return r.setVolumeGroupStatus(ctx, vg, status)
}

func (r *VGReconciler) setVolumeGroupStatus(ctx context.Context, vg *lvmv1alpha1.LVMVolumeGroup, status *lvmv1alpha1.VGStatus) error {
	logger := log.FromContext(ctx).WithValues("VolumeGroup", client.ObjectKeyFromObject(vg))

	// Get LVMVolumeGroupNodeStatus and set the relevant VGStatus
	nodeStatus := r.getLVMVolumeGroupNodeStatus()

	result, err := ctrl.CreateOrUpdate(ctx, r.Client, nodeStatus, func() error {
		// set an owner instead of a controller reference, as there can be multiple volume groups.
		if err := controllerutil.SetOwnerReference(nodeStatus, vg, r.Scheme); err != nil {
			logger.Error(err, "failed to set owner-reference when updating volume-group status")
		}

		exists := false
		for i, existingVGStatus := range nodeStatus.Spec.LVMVGStatus {
			if existingVGStatus.Name == status.Name {
				exists = true
				nodeStatus.Spec.LVMVGStatus[i] = *status
			}
		}
		if !exists {
			nodeStatus.Spec.LVMVGStatus = append(nodeStatus.Spec.LVMVGStatus, *status)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("LVMVolumeGroupNodeStatus could not be updated: %w", err)
	}

	if result != controllerutil.OperationResultNone {
		logger.Info("LVMVolumeGroupNodeStatus modified", "operation", result, "name", nodeStatus.Name)
	} else {
		logger.Info("LVMVolumeGroupNodeStatus unchanged")
	}

	return nil
}

func (r *VGReconciler) removeVolumeGroupStatus(ctx context.Context, vg *lvmv1alpha1.LVMVolumeGroup) error {
	logger := log.FromContext(ctx)

	// Get LVMVolumeGroupNodeStatus and remove the relevant VGStatus
	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.NodeName,
			Namespace: r.Namespace,
		},
	}

	exist := false
	index := 0
	result, err := ctrl.CreateOrUpdate(ctx, r.Client, nodeStatus, func() error {
		for i, existingVGStatus := range nodeStatus.Spec.LVMVGStatus {
			if existingVGStatus.Name == vg.GetName() {
				exist = true
				index = i
			}
		}

		if exist {
			nodeStatus.Spec.LVMVGStatus = append(nodeStatus.Spec.LVMVGStatus[:index], nodeStatus.Spec.LVMVGStatus[index+1:]...)
			// if we remove the vgstatus, we also remove the owner reference
			for ownerRefIndex, ownerRef := range nodeStatus.GetOwnerReferences() {
				if ownerRef.UID == vg.GetUID() {
					nodeStatus.SetOwnerReferences(append(
						nodeStatus.GetOwnerReferences()[:ownerRefIndex],
						nodeStatus.GetOwnerReferences()[ownerRefIndex+1:]...),
					)
					break
				}
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update LVMVolumeGroupNodeStatus %s", nodeStatus.GetName())
	}

	if result != controllerutil.OperationResultNone {
		logger.Info("LVMVolumeGroupNodeStatus modified", "operation", result, "name", nodeStatus.Name)
	} else {
		logger.Info("LVMVolumeGroupNodeStatus unchanged")
	}

	return nil
}

func (r *VGReconciler) setDevices(status *lvmv1alpha1.VGStatus) (bool, error) {
	vgs, err := lvm.ListVolumeGroups(r.executor)
	if err != nil {
		return false, fmt.Errorf("failed to list volume groups. %v", err)
	}

	devicesExist := false
	for _, vg := range vgs {
		if status.Name == vg.Name {
			if len(vg.PVs) > 0 {
				devicesExist = true
				status.Devices = make([]string, len(vg.PVs))
				for i := range vg.PVs {
					status.Devices[i] = vg.PVs[i].PvName
				}
			}
		}
	}

	return devicesExist, nil
}

func (r *VGReconciler) getLVMVolumeGroupNodeStatus() *lvmv1alpha1.LVMVolumeGroupNodeStatus {
	return &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.NodeName,
			Namespace: r.Namespace,
		},
	}
}
