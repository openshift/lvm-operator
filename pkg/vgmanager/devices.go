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
	"fmt"
	"path/filepath"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/pkg/internal"
)

// addDevicesToVG creates or extends a volume group using the provided devices.
func (r *VGReconciler) addDevicesToVG(vgs []VolumeGroup, vgName string, devices []internal.BlockDevice) error {
	if len(devices) < 1 {
		return fmt.Errorf("can't create vg %q with 0 devices", vgName)
	}

	// check if volume group is already present
	vgFound := false
	for _, vg := range vgs {
		if vg.Name == vgName {
			vgFound = true
		}
	}

	// TODO: Check if we can use functions from lvm.go here
	var cmd string
	if vgFound {
		r.Log.Info("extending an existing volume group", "VGName", vgName)
		cmd = "/usr/sbin/vgextend"
	} else {
		r.Log.Info("creating a new volume group", "VGName", vgName)
		cmd = "/usr/sbin/vgcreate"
	}

	args := []string{vgName}
	for _, device := range devices {
		if device.DevicePath != "" {
			args = append(args, device.DevicePath)
		} else {
			args = append(args, device.KName)
		}
	}

	_, err := r.executor.ExecuteCommandWithOutputAsHost(cmd, args...)
	if err != nil {
		return fmt.Errorf("failed to create or extend volume group %q using command '%s': %v", vgName, fmt.Sprintf("%s %s", cmd, strings.Join(args, " ")), err)
	}

	return nil
}

// getAvailableDevicesForVG determines the available devices that can be used to create a volume group.
func (r *VGReconciler) getAvailableDevicesForVG(blockDevices []internal.BlockDevice, vgs []VolumeGroup, volumeGroup *lvmv1alpha1.LVMVolumeGroup) ([]internal.BlockDevice, []internal.BlockDevice, error) {
	// filter devices based on DeviceSelector.Paths if specified
	availableDevices, err := r.filterMatchingDevices(blockDevices, vgs, volumeGroup)
	if err != nil {
		r.Log.Error(err, "failed to filter matching devices", "VGName", volumeGroup.Name)
		return nil, nil, err
	}

	// determine only available devices based on device age and filters in FilterMap
	availableDevices, delayedDevices := r.filterAvailableDevices(availableDevices)

	return availableDevices, delayedDevices, nil
}

// filterAvailableDevices returns:
// availableDevices: the list of blockdevices considered available
// delayedDevices: the list of blockdevices considered available, but first observed less than 'minDeviceAge' time ago
func (r *VGReconciler) filterAvailableDevices(blockDevices []internal.BlockDevice) ([]internal.BlockDevice, []internal.BlockDevice) {
	var availableDevices, delayedDevices []internal.BlockDevice
	// using a label so `continue DeviceLoop` can be used to skip devices
DeviceLoop:
	for _, blockDevice := range blockDevices {

		// store device in deviceAgeMap
		r.deviceAgeMap.storeDeviceAge(blockDevice.KName)

		// check for partitions recursively
		if blockDevice.HasChildren() {
			childAvailableDevices, childDelayedDevices := r.filterAvailableDevices(blockDevice.Children)
			availableDevices = append(availableDevices, childAvailableDevices...)
			delayedDevices = append(delayedDevices, childDelayedDevices...)
		}

		devLogger := r.Log.WithValues("Device.Name", blockDevice.Name)
		for name, filter := range FilterMap {
			filterLogger := devLogger.WithValues("filter.Name", name)
			valid, err := filter(blockDevice, r.executor)
			if err != nil {
				filterLogger.Error(err, "filter error")
				continue DeviceLoop
			} else if !valid {
				filterLogger.Info("does not match filter")
				continue DeviceLoop
			}
		}
		// check if the device is older than deviceMinAge
		isOldEnough := r.deviceAgeMap.isOlderThan(blockDevice.KName)
		if isOldEnough {
			availableDevices = append(availableDevices, blockDevice)
		} else {
			delayedDevices = append(delayedDevices, blockDevice)
		}
	}
	return availableDevices, delayedDevices
}

// filterMatchingDevices filters devices based on DeviceSelector.Paths if specified.
func (r *VGReconciler) filterMatchingDevices(blockDevices []internal.BlockDevice, vgs []VolumeGroup, volumeGroup *lvmv1alpha1.LVMVolumeGroup) ([]internal.BlockDevice, error) {
	var filteredBlockDevices []internal.BlockDevice
	if volumeGroup.Spec.DeviceSelector != nil && len(volumeGroup.Spec.DeviceSelector.Paths) > 0 {
		for _, path := range volumeGroup.Spec.DeviceSelector.Paths {
			diskName, err := filepath.EvalSymlinks(path)
			if err != nil {
				err = fmt.Errorf("unable to find symlink for disk path %s: %v", path, err)
				return []internal.BlockDevice{}, err
			}

			isAlreadyExist := isDeviceAlreadyPartOfVG(vgs, diskName, volumeGroup)
			if isAlreadyExist {
				continue
			}

			blockDevice, ok := hasExactDisk(blockDevices, diskName)
			if !ok {
				return []internal.BlockDevice{}, fmt.Errorf("can not find device name %s in the available block devices", path)
			}

			blockDevice.DevicePath = path
			filteredBlockDevices = append(filteredBlockDevices, blockDevice)
		}

		return filteredBlockDevices, nil
	}

	// return all available block devices if none is specified in the CR
	return blockDevices, nil
}

func isDeviceAlreadyPartOfVG(vgs []VolumeGroup, diskName string, volumeGroup *lvmv1alpha1.LVMVolumeGroup) bool {
	for _, vg := range vgs {
		if vg.Name == volumeGroup.Name {
			for _, pv := range vg.PVs {
				if pv == diskName {
					return true
				}
			}
		}
	}

	return false
}

func hasExactDisk(blockDevices []internal.BlockDevice, deviceName string) (internal.BlockDevice, bool) {
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
	return internal.BlockDevice{}, false
}
