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

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// addDevicesToVG creates or extends a volume group using the provided devices.
func (r *Reconciler) addDevicesToVG(ctx context.Context, vgs []lvm.VolumeGroup, vg *lvmv1alpha1.LVMVolumeGroup, devices []lsblk.BlockDevice) error {
	logger := log.FromContext(ctx)
	vgName := vg.Name

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
		args = append(args, device.KName)
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
		if err := r.LVM.CreateVG(ctx, lvm.VolumeGroup{Name: vgName, PVs: pvs}, lvm.CreateVGOptions{
			lvm.SharedVGOptions{
				DeviceAccessPolicy:  vg.Spec.DeviceAccessPolicy,
				OwnsGlobalLockSpace: r.IsSharedVGLeader,
			},
		}); err != nil {
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

// VerifyMandatoryDevicePaths verifies if the provided device list is either available or already setup correctly.
// While availability is easy to determine, an exclusion by being already setup can only be determined by
// checking if the excluded device has been filtered due to filter.ErrDeviceAlreadySetupCorrectly.
func VerifyMandatoryDevicePaths(f FilteredBlockDevices, resolver *symlinkResolver.Resolver, paths []v1alpha1.DevicePath) error {
	for _, path := range paths {
		path, err := resolver.Resolve(path.Unresolved())
		if err != nil {
			return fmt.Errorf("failed to resolve symlink to determine available or setup path: %w", err)
		}
		available := f.IsAvailable(path)
		errs := f.FilterErrors(path)
		alreadySetup := false
		for _, err := range errs {
			if errors.Is(err, filter.ErrDeviceAlreadySetupCorrectly) {
				alreadySetup = true
				break
			}
		}
		if !available && !alreadySetup {
			if len(errs) > 0 {
				err = errors.Join(errs...)
			} else {
				err = fmt.Errorf("the device did not exist on the host, "+
					"make sure it is connected and visible via \"lsblk --json --paths -o %s\" "+
					"and confirm it is discoverable via a path resolvable (e.g. via symlink) on the host", lsblk.LSBLK_COLUMNS)
			}
			return fmt.Errorf("mandatory device path %q cannot be used, "+
				"because it is NOT available as a new device for the volume group and "+
				"NOT part of a valid and tagged existing volume group: %w",
				path,
				err,
			)
		}
	}
	return nil
}

// IsAvailable checks if the provided device is available for use in a new volume group.
func (f FilteredBlockDevices) IsAvailable(dev string) bool {
	for _, available := range f.Available {
		if dev == available.KName {
			return true
		}
	}
	return false
}

// FilterErrors checks if the provided device is already setup correctly in an existing volume group
// (all filters decided the device is either okay to use or returned filter.ErrDeviceAlreadySetupCorrectly).
func (f FilteredBlockDevices) FilterErrors(dev string) []error {
	for _, excluded := range f.Excluded {
		if dev == excluded.KName {
			return excluded.FilterErrors
		}
	}
	return nil
}

// filterDevices returns:
// availableDevices: the list of blockdevices considered available
func filterDevices(ctx context.Context, devices []lsblk.BlockDevice, resolver *symlinkResolver.Resolver, filters filter.Filters) FilteredBlockDevices {
	logger := log.FromContext(ctx)

	availableByKName := make(map[string]lsblk.BlockDevice)
	excludedByKName := make(map[string]FilteredBlockDevice)

	for _, device := range devices {
		// check for partitions recursively
		if device.HasChildren() {
			filteredChildDevices := filterDevices(ctx, device.Children, resolver, filters)
			for _, available := range filteredChildDevices.Available {
				if _, exists := availableByKName[available.KName]; exists {
					logger.WithValues("device.KName", available.KName).
						V(3).Info("skipping duplicate available device, this can happen for multipath / dmsetup devices")
				} else {
					availableByKName[available.KName] = available
				}
			}
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
			if err := filterFunc(device, resolver); err != nil {
				logger.WithValues("device.KName", device.KName, "filter.Name", name).
					V(3).Info("excluded", "reason", err)
				filterErrs = append(filterErrs, err)
			}
		}
		if len(filterErrs) == 0 {
			availableByKName[device.KName] = device
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

	availableDevices := make([]lsblk.BlockDevice, 0, len(availableByKName))
	for _, device := range availableByKName {
		availableDevices = append(availableDevices, device)
	}

	return FilteredBlockDevices{
		Available: availableDevices,
		Excluded:  excluded,
	}
}
