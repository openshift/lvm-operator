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
	"errors"
	"fmt"
	"path/filepath"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// addDevicesToVG creates or extends a volume group using the provided devices.
func (r *Reconciler) addDevicesToVG(ctx context.Context, vgs []lvm.VolumeGroup, vgName string, devices []lsblk.BlockDevice) error {
	logger := log.FromContext(ctx)

	if len(devices) < 1 {
		return fmt.Errorf("can't create vg %q with 0 devices", vgName)
	}

	// check if volume group is already present
	var existingVolumeGroup *lvm.VolumeGroup
	for _, vg := range vgs {
		if vg.Name == vgName {
			existingVolumeGroup = &vg
		}
	}

	var args []string
	for _, device := range devices {
		if device.DevicePath != "" {
			args = append(args, device.DevicePath)
		} else {
			args = append(args, device.KName)
		}
	}

	if existingVolumeGroup != nil {
		logger.Info("extending an existing volume group", "VGName", vgName)
		if _, err := r.LVM.ExtendVG(ctx, *existingVolumeGroup, args); err != nil {
			return fmt.Errorf("failed to extend volume group %s: %w", vgName, err)
		}
	} else {
		logger.Info("creating a new volume group", "VGName", vgName)
		var pvs []lvm.PhysicalVolume
		for _, pvName := range args {
			pvs = append(pvs, lvm.PhysicalVolume{PvName: pvName})
		}
		if err := r.LVM.CreateVG(ctx, lvm.VolumeGroup{Name: vgName, PVs: pvs}); err != nil {
			return fmt.Errorf("failed to create volume group %s: %w", vgName, err)
		}
	}
	return nil
}

type FilteredBlockDevice struct {
	lsblk.BlockDevice
	FilterErrors []error
}

type FilteredBlockDevices struct {
	Available []lsblk.BlockDevice
	Excluded  []FilteredBlockDevice
}

// filterDevices returns:
// availableDevices: the list of blockdevices considered available
func (r *Reconciler) filterDevices(ctx context.Context, devices []lsblk.BlockDevice, pvs []lvm.PhysicalVolume, bdi lsblk.BlockDeviceInfos, filters filter.Filters) FilteredBlockDevices {
	logger := log.FromContext(ctx)

	var availableDevices []lsblk.BlockDevice
	excludedByKName := make(map[string]FilteredBlockDevice)

	for _, device := range devices {
		// check for partitions recursively
		if device.HasChildren() {
			filteredChildDevices := r.filterDevices(ctx, device.Children, pvs, bdi, filters)
			availableDevices = append(availableDevices, filteredChildDevices.Available...)
			for _, excludedChildDevice := range filteredChildDevices.Excluded {
				if excluded, ok := excludedByKName[excludedChildDevice.KName]; ok {
					excluded.FilterErrors = append(excluded.FilterErrors, excludedChildDevice.FilterErrors...)
				} else {
					excludedByKName[excludedChildDevice.KName] = excludedChildDevice
				}
			}
		}

		filterErrs := make([]error, 0, len(filters))
		for name, filterFunc := range filters {
			if err := filterFunc(device, pvs, bdi); err != nil {
				logger.WithValues("device.KName", device.KName, "filter.Name", name).
					V(3).Info("excluded", "reason", err)
				filterErrs = append(filterErrs, err)
			}
		}
		if len(filterErrs) == 0 {
			availableDevices = append(availableDevices, device)
			continue
		}

		filtered, found := excludedByKName[device.KName]
		if found {
			filtered.FilterErrors = append(filtered.FilterErrors, filterErrs...)
		} else {
			excludedByKName[device.KName] = FilteredBlockDevice{device, filterErrs}
		}
	}

	var excluded []FilteredBlockDevice
	for _, device := range excludedByKName {
		excluded = append(excluded, device)
	}

	return FilteredBlockDevices{
		Available: availableDevices,
		Excluded:  excluded,
	}
}

// getNewDevicesToBeAdded gets all devices that should be added to the volume group
func (r *Reconciler) getNewDevicesToBeAdded(ctx context.Context, blockDevices []lsblk.BlockDevice, nodeStatus *lvmv1alpha1.LVMVolumeGroupNodeStatus, volumeGroup *lvmv1alpha1.LVMVolumeGroup) ([]lsblk.BlockDevice, error) {
	logger := log.FromContext(ctx)

	var validBlockDevices []lsblk.BlockDevice
	atLeastOneDeviceIsAlreadyInVolumeGroup := false

	if volumeGroup.Spec.DeviceSelector == nil {
		// return all available block devices if none is specified in the CR
		return blockDevices, nil
	}

	// If Paths is specified, treat it as required paths
	for _, path := range volumeGroup.Spec.DeviceSelector.Paths {
		blockDevice, err := getValidDevice(path, blockDevices, nodeStatus, volumeGroup)
		if err != nil {
			// An error for required devices is critical
			return nil, fmt.Errorf("unable to validate device %s: %v", path, err)
		}

		// Check if we should skip this device
		if blockDevice.DevicePath == "" {
			logger.Info(fmt.Sprintf("skipping required device that is already part of volume group %s: %s", volumeGroup.Name, path))
			atLeastOneDeviceIsAlreadyInVolumeGroup = true
			continue
		}

		validBlockDevices = append(validBlockDevices, blockDevice)
	}

	for _, path := range volumeGroup.Spec.DeviceSelector.OptionalPaths {
		blockDevice, err := getValidDevice(path, blockDevices, nodeStatus, volumeGroup)

		// Check if we should skip this device
		if err != nil {
			logger.Info(fmt.Sprintf("skipping optional device path: %v", err))
			continue
		}

		// Check if we should skip this device
		if blockDevice.DevicePath == "" {
			logger.Info(fmt.Sprintf("skipping optional device path that is already part of volume group %s: %s", volumeGroup.Name, path))
			atLeastOneDeviceIsAlreadyInVolumeGroup = true
			continue
		}

		validBlockDevices = append(validBlockDevices, blockDevice)
	}

	// Check for any optional paths
	// At least 1 of the optional paths are required if:
	//   - OptionalPaths was specified AND
	//   - There were no required paths
	//   - Devices were not already part of the volume group (meaning this was run after vg creation)
	// This guarantees at least 1 device could be found between optionalPaths and paths
	// if len(FilteredBlockDevices) == 0 && !atLeastOneDeviceIsAlreadyInVolumeGroup {
	if len(validBlockDevices) == 0 && !atLeastOneDeviceIsAlreadyInVolumeGroup {
		return nil, errors.New("at least 1 valid device is required if DeviceSelector paths or optionalPaths are specified")
	}

	return validBlockDevices, nil
}

func isDeviceAlreadyPartOfVG(nodeStatus *lvmv1alpha1.LVMVolumeGroupNodeStatus, diskName string, volumeGroup *lvmv1alpha1.LVMVolumeGroup) bool {
	if nodeStatus == nil {
		return false
	}
	for _, vgStatus := range nodeStatus.Spec.LVMVGStatus {
		if vgStatus.Name == volumeGroup.Name {
			for _, pv := range vgStatus.Devices {
				if resolvedPV, _ := evalSymlinks(pv); resolvedPV == diskName {
					return true
				}
			}
		}
	}

	return false
}

func hasExactDisk(blockDevices []lsblk.BlockDevice, deviceName string) (lsblk.BlockDevice, bool) {
	for _, blockDevice := range blockDevices {
		if blockDevice.KName == deviceName {
			return blockDevice, true
		}
		if blockDevice.HasChildren() {
			if device, ok := hasExactDisk(blockDevice.Children, deviceName); ok {
				return device, true
			}
		}
	}
	return lsblk.BlockDevice{}, false
}

// evalSymlinks redefined to be able to override in tests
var evalSymlinks = filepath.EvalSymlinks

// getValidDevice will do various checks on a device path to make sure it is a valid device
//
//	An error will be returned if the device is invalid
//	No error and an empty BlockDevice object will be returned if this device should be skipped (ex: duplicate device)
func getValidDevice(devicePath string, blockDevices []lsblk.BlockDevice, nodeStatus *lvmv1alpha1.LVMVolumeGroupNodeStatus, volumeGroup *lvmv1alpha1.LVMVolumeGroup) (lsblk.BlockDevice, error) {
	// Make sure the symlink exists
	diskName, err := evalSymlinks(devicePath)
	if err != nil {
		return lsblk.BlockDevice{}, fmt.Errorf("unable to find symlink for disk path %s: %v", devicePath, err)
	}

	// Make sure this isn't a duplicate in the VG
	if isDeviceAlreadyPartOfVG(nodeStatus, diskName, volumeGroup) {
		return lsblk.BlockDevice{}, nil // No error, we just don't want a duplicate
	}

	// Make sure the block device exists
	blockDevice, ok := hasExactDisk(blockDevices, diskName)
	if !ok {
		return lsblk.BlockDevice{}, fmt.Errorf("can not find device name %s in the available block devices", devicePath)
	}

	blockDevice.DevicePath = devicePath
	return blockDevice, nil
}
