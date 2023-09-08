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

package filter

import (
	"fmt"
	"strings"

	"github.com/openshift/lvm-operator/pkg/internal"
	"github.com/openshift/lvm-operator/pkg/lvm"
)

const (
	// filter names:
	notReadOnly                = "notReadOnly"
	notSuspended               = "notSuspended"
	noBiosBootInPartLabel      = "noBiosBootInPartLabel"
	noReservedInPartLabel      = "noReservedInPartLabel"
	noValidFilesystemSignature = "noValidFilesystemSignature"
	noBindMounts               = "noBindMounts"
	noChildren                 = "noChildren"
	usableDeviceType           = "usableDeviceType"
)

// maps of function identifier (for logs) to filter function.
// These are passed the localv1alpha1.DeviceInclusionSpec to make testing easier,
// but they aren't expected to use it
// they verify that the device itself is good to use
var FilterMap = map[string]func(internal.BlockDevice, internal.Executor) (bool, error){
	notReadOnly: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		return !dev.ReadOnly, nil
	},

	notSuspended: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		matched := dev.State != internal.StateSuspended
		return matched, nil
	},

	noBiosBootInPartLabel: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		biosBootInPartLabel := strings.Contains(strings.ToLower(dev.PartLabel), strings.ToLower("bios")) ||
			strings.Contains(strings.ToLower(dev.PartLabel), strings.ToLower("boot"))
		return !biosBootInPartLabel, nil
	},

	noReservedInPartLabel: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		reservedInPartLabel := strings.Contains(strings.ToLower(dev.PartLabel), "reserved")
		return !reservedInPartLabel, nil
	},

	noValidFilesystemSignature: func(dev internal.BlockDevice, e internal.Executor) (bool, error) {
		// if no fs type is set, it's always okay
		if dev.FSType == "" {
			return true, nil
		}

		// if fstype is set to LVM2_member then it already was created as a PV
		// this means that if the disk has no children, we can safely reuse it if it's a valid LVM PV.
		if dev.FSType == "LVM2_member" && !dev.HasChildren() {
			pvs, err := lvm.ListPhysicalVolumes(e, "")
			if err != nil {
				return false, fmt.Errorf("could not determine if block device has valid filesystem signature, since it is flagged as LVM2_member but physical volumes could not be verified: %w", err)
			}

			for _, pv := range pvs {
				// a volume is a valid PV if it has the same name as the block device and no associated volume group and has available disk space
				// however if there is a PV that matches the Device and there is a VG associated with it or no available space, we cannot use it
				if pv.PvName == dev.KName {
					if pv.VgName == "" && pv.PvFree != "0G" {
						return true, nil
					} else {
						return false, nil
					}
				}
			}

			// if there was no PV that matched it and it still is flagged as LVM2_member, it is formatted but not recognized by LVM
			// configuration. We can assume that in this case, the Volume can be reused by simply recalling the vgcreate command on it
			return true, nil
		}
		return false, nil
	},

	noBindMounts: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		hasBindMounts, _, err := dev.HasBindMounts()
		return !hasBindMounts, err
	},

	noChildren: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		hasChildren := dev.HasChildren()
		return !hasChildren, nil
	},

	usableDeviceType: func(dev internal.BlockDevice, executor internal.Executor) (bool, error) {
		switch dev.Type {
		case internal.DeviceTypeLoop:
			// check loop device isn't being used by kubernetes
			return dev.IsUsableLoopDev(executor)
		case internal.DeviceTypeROM:
			return false, nil
		case internal.DeviceTypeLVM:
			return false, nil
		default:
			return true, nil
		}
	},
}
