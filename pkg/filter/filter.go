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

	"github.com/openshift/lvm-operator/pkg/lsblk"
	"github.com/openshift/lvm-operator/pkg/lvm"
)

const (
	// StateSuspended is a possible value of BlockDevice.State
	StateSuspended = "suspended"

	// DeviceTypeLoop is the device type for loop devices in lsblk output
	DeviceTypeLoop = "loop"

	// DeviceTypeROM is the device type for ROM devices in lsblk output
	DeviceTypeROM = "rom"

	// DeviceTypeLVM is the device type for lvm devices in lsblk output
	DeviceTypeLVM = "lvm"
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

type Filters map[string]func(lsblk.BlockDevice) (bool, error)

func DefaultFilters(lvm lvm.LVM, lsblkInstance lsblk.LSBLK) Filters {
	return Filters{
		notReadOnly: func(dev lsblk.BlockDevice) (bool, error) {
			return !dev.ReadOnly, nil
		},

		notSuspended: func(dev lsblk.BlockDevice) (bool, error) {
			matched := dev.State != StateSuspended
			return matched, nil
		},

		noBiosBootInPartLabel: func(dev lsblk.BlockDevice) (bool, error) {
			biosBootInPartLabel := strings.Contains(strings.ToLower(dev.PartLabel), strings.ToLower("bios")) ||
				strings.Contains(strings.ToLower(dev.PartLabel), strings.ToLower("boot"))
			return !biosBootInPartLabel, nil
		},

		noReservedInPartLabel: func(dev lsblk.BlockDevice) (bool, error) {
			reservedInPartLabel := strings.Contains(strings.ToLower(dev.PartLabel), "reserved")
			return !reservedInPartLabel, nil
		},

		noValidFilesystemSignature: func(dev lsblk.BlockDevice) (bool, error) {
			// if no fs type is set, it's always okay
			if dev.FSType == "" {
				return true, nil
			}

			// if fstype is set to LVM2_member then it already was created as a PV
			// this means that if the disk has no children, we can safely reuse it if it's a valid LVM PV.
			if dev.FSType == "LVM2_member" && !dev.HasChildren() {
				pvs, err := lvm.ListPVs("")
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

		noChildren: func(dev lsblk.BlockDevice) (bool, error) {
			hasChildren := dev.HasChildren()
			return !hasChildren, nil
		},

		noBindMounts: func(dev lsblk.BlockDevice) (bool, error) {
			hasBindMounts, _, err := lsblkInstance.HasBindMounts(dev)
			return !hasBindMounts, err
		},

		usableDeviceType: func(dev lsblk.BlockDevice) (bool, error) {
			switch dev.Type {
			case DeviceTypeLoop:
				// check loop device isn't being used by kubernetes
				return lsblkInstance.IsUsableLoopDev(dev)
			case DeviceTypeROM:
				return false, nil
			case DeviceTypeLVM:
				return false, nil
			default:
				return true, nil
			}
		},
	}
}
