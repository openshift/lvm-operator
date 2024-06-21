package vgmanager

import (
	"context"
	"errors"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/dmsetup"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *Reconciler) wipeDevicesIfNecessary(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup, nodeStatus *lvmv1alpha1.LVMVolumeGroupNodeStatus, blockDevices []lsblk.BlockDevice) (bool, error) {
	logger := log.FromContext(ctx)

	if volumeGroup.Spec.DeviceSelector == nil || volumeGroup.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData == nil || !*volumeGroup.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData {
		return false, nil
	}

	wiped := false
	for _, path := range volumeGroup.Spec.DeviceSelector.Paths {
		diskName, err := evalSymlinks(path)
		if err != nil {
			return false, fmt.Errorf("unable to find symlink for disk path %s: %v", path, err)
		}
		if isDeviceAlreadyPartOfVG(nodeStatus, diskName, volumeGroup) {
			continue
		}
		deviceWiped, err := r.wipeDevice(ctx, diskName, blockDevices)
		if err != nil {
			return false, fmt.Errorf("failed to wipe device %s: %w", path, err)
		}
		if deviceWiped {
			wiped = true
		}
	}
	for _, path := range volumeGroup.Spec.DeviceSelector.OptionalPaths {
		diskName, err := evalSymlinks(path)
		if err != nil {
			logger.Info(fmt.Sprintf("skipping wiping optional device %s as unable to find symlink: %v", path, err))
			continue
		}
		if isDeviceAlreadyPartOfVG(nodeStatus, diskName, volumeGroup) {
			continue
		}
		deviceWiped, err := r.wipeDevice(ctx, diskName, blockDevices)
		if err != nil {
			logger.Info(fmt.Sprintf("skipping wiping optional device %s: %v", path, err))
		}
		if deviceWiped {
			wiped = true
		}
	}

	return wiped, nil
}

func (r *Reconciler) wipeDevice(ctx context.Context, deviceName string, blockDevices []lsblk.BlockDevice) (bool, error) {
	logger := log.FromContext(ctx).WithValues("deviceName", deviceName)

	wiped := false
	for _, device := range blockDevices {
		if device.KName == deviceName {
			if err := r.Wipefs.Wipe(ctx, device.KName); err != nil {
				return false, err
			}
			wiped = true
			logger.Info("device wiped successfully")
			for _, child := range device.Children {
				// If the device was used as a Physical Volume before, wipefs does not remove the child LVs.
				// So, a device-mapper reference removal is necessary to further remove the child LV references.
				r.removeMapperReference(ctx, child)
			}
		} else if device.HasChildren() {
			childWiped, err := r.wipeDevice(ctx, deviceName, device.Children)
			if err != nil {
				return false, err
			}
			if childWiped {
				wiped = true
			}
		}
	}
	return wiped, nil
}

// removeMapperReference remove the device-mapper reference of the device starting from the most inner child
func (r *Reconciler) removeMapperReference(ctx context.Context, device lsblk.BlockDevice) {
	logger := log.FromContext(ctx).WithValues("deviceName", device.KName)
	if device.HasChildren() {
		for _, child := range device.Children {
			r.removeMapperReference(ctx, child)
		}
	}
	if err := r.Dmsetup.Remove(ctx, device.KName); err != nil {
		if errors.Is(err, dmsetup.ErrReferenceNotFound) {
			logger.Info("skipping the removal of device-mapper reference as the reference does not exist", "childName", device.KName)
		} else {
			logger.Info("failed to remove device-mapper reference", "childName", device.KName, "error", err)
		}
	} else {
		logger.Info("device-mapper reference removed successfully", "childName", device.KName)
	}
}
