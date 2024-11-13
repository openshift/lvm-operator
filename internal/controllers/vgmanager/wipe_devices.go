package vgmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/constants"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/dmsetup"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *Reconciler) wipeDevices(
	ctx context.Context,
	volumeGroup *lvmv1alpha1.LVMVolumeGroup,
	blockDevices []lsblk.BlockDevice,
) (bool, error) {
	logger := log.FromContext(ctx)

	if !r.shouldWipeDevicesOnVolumeGroup(volumeGroup) {
		logger.V(1).Info("skipping wiping devices as the volume group does not require it")
		return false, nil
	}

	updated := false

	for _, path := range volumeGroup.Spec.DeviceSelector.Paths {
		if deviceWiped, err := r.wipeDevice(ctx, path, blockDevices); err != nil {
			return false, fmt.Errorf("failed to wipe device %s: %w", path, err)
		} else if deviceWiped {
			updated = true
		}
	}
	for _, path := range volumeGroup.Spec.DeviceSelector.OptionalPaths {
		if deviceWiped, err := r.wipeDevice(ctx, path, blockDevices); err != nil {
			logger.Info(fmt.Sprintf("skipping wiping optional device %s: %v", path, err))
		} else if deviceWiped {
			updated = true
		}
	}

	if updated {
		if volumeGroup.Annotations == nil {
			volumeGroup.Annotations = make(map[string]string)
		}
		volumeGroup.Annotations[constants.DevicesWipedAnnotationPrefix+r.NodeName] = fmt.Sprintf(
			"the devices of this volume group have been wiped at %s by lvms according to policy. This marker"+
				"serves as indicator that the devices have been wiped before and should not be wiped again."+
				"removal of this annotation is unsupported and may lead to data loss due to additional wiping.",
			time.Now().Format(time.RFC3339))
	}

	return updated, nil
}

// shouldWipeDevicesOnVolumeGroup checks if the volume group should have its devices wiped
// based on the ForceWipeDevicesAndDestroyAllData field in the DeviceSelector.
// If the field is not set, it returns false.
// If the field is set to false, it returns false.
// If the field is set to true, it returns true if the volume group has not been wiped before.
func (r *Reconciler) shouldWipeDevicesOnVolumeGroup(vg *lvmv1alpha1.LVMVolumeGroup) bool {
	// If the volume group does not have the DeviceSelector field, it should not be wiped because it is unsafe.
	// If devices are detected at runtime, wiping can lead to data loss.
	if vg.Spec.DeviceSelector == nil {
		return false
	}
	// If the volume group has the DeviceSelector field but the ForceWipeDevicesAndDestroyAllData field is not set,
	// it should not be wiped because it was disabled by the user (or not intended)
	if vg.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData == nil {
		return false
	}
	if !*vg.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData {
		return false
	}

	// If the wiped annotation is present, the devices have been wiped before.
	// If the devices have not been wiped before, they should be wiped.
	var wipedBefore bool
	if vg.Annotations != nil {
		_, wipedBefore = vg.Annotations[constants.DevicesWipedAnnotationPrefix+r.NodeName]
	}

	return !wipedBefore
}

func (r *Reconciler) wipeDevice(ctx context.Context, deviceName string, blockDevices []lsblk.BlockDevice) (bool, error) {
	logger := log.FromContext(ctx).WithValues("deviceName", deviceName)

	var err error
	if deviceName, err = evalSymlinks(deviceName); err != nil {
		return false, fmt.Errorf("failed to evaluate symlink for device %s: %w", deviceName, err)
	}

	wiped := false
	for _, device := range blockDevices {
		if device.KName == deviceName {
			// remove all references that were just orphaned
			for _, child := range device.Children {
				// all mapper references must be removed before wiping the device
				r.removeMapperReference(ctx, child)
			}
			logger.Info("wipe device", "deviceName", deviceName)
			// wipe all signatures once more and cause ioctl reload
			if err := r.Wipefs.Wipe(ctx, device.KName); err != nil {
				return false, err
			}
			logger.Info("device wiped successfully")
			wiped = true
			break
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
	if device.Type == "part" {
		logger.Info("skipping the removal of device-mapper reference as the device is a partition", "childName", device.KName)
		return
	} else {
		logger.Info("removing device-mapper reference", "childName", device.KName, "deviceType", device.Type)
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
