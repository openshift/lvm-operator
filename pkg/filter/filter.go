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
	"errors"
	"fmt"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
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

const FSTypeLVM2Member = "LVM2_member"

const (
	// filter names:
	notReadOnly                   = "notReadOnly"
	notSuspended                  = "notSuspended"
	noInvalidPartitionLabel       = "noInvalidPartitionLabel"
	onlyValidFilesystemSignatures = "onlyValidFilesystemSignatures"
	noBindMounts                  = "noBindMounts"
	noChildren                    = "noChildren"
	usableDeviceType              = "usableDeviceType"
)

var (
	ErrDeviceAlreadySetupCorrectly = errors.New("the device is already setup correctly and was filtered to avoid attempting recreation")
	ErrLVMPartition                = errors.New("the device is a lvm partition and is excluded by default")
)

var (
	invalidPartitionLabels = []string{
		"bios", "boot", "reserved",
	}
)

type Filter func(lsblk.BlockDevice) error

type Filters map[string]Filter

// IsExpectedDeviceErrorAfterSetup can be used to check for errors in the filtering process that are expected after
// the volume group has been initialized correctly because the devices for the volume group create new devices or
// change their attributes.
func IsExpectedDeviceErrorAfterSetup(err error) bool {
	return errors.Is(err, ErrDeviceAlreadySetupCorrectly) || errors.Is(err, ErrLVMPartition)
}

func DefaultFilters(vg *lvmv1alpha1.LVMVolumeGroup, lvmInstance lvm.LVM, lsblkInstance lsblk.LSBLK) Filters {
	return Filters{
		notReadOnly: func(dev lsblk.BlockDevice) error {
			if dev.ReadOnly {
				return fmt.Errorf("%s cannot be read-only", dev.Name)
			}
			return nil
		},

		notSuspended: func(dev lsblk.BlockDevice) error {
			if dev.State == StateSuspended {
				return fmt.Errorf("%s cannot be in a %q state", dev.State, dev.Name)
			}
			return nil
		},

		noInvalidPartitionLabel: func(dev lsblk.BlockDevice) error {
			for _, invalidLabel := range invalidPartitionLabels {
				if strings.Contains(strings.ToLower(dev.PartLabel), invalidLabel) {
					return fmt.Errorf("%s has an invalid partition label %q", dev.Name, dev.PartLabel)
				}
			}
			return nil
		},

		onlyValidFilesystemSignatures: func(dev lsblk.BlockDevice) error {
			// if no fs type is set, it's always okay
			if dev.FSType == "" {
				return nil
			}

			// if fstype is set to LVM2_member then it already was created as a PV
			// this means that if the disk has no children, we can safely reuse it if it's a valid LVM PV.
			if dev.FSType == FSTypeLVM2Member {
				pvs, err := lvmInstance.ListPVs("")
				if err != nil {
					return fmt.Errorf("could not determine if %s has a valid filesystem signature. It is flagged as a LVM2_member but the physical volumes could not be verified: %w", dev.Name, err)
				}

				var foundPV *lvm.PhysicalVolume
				for _, pv := range pvs {
					if pv.PvName == dev.KName {
						foundPV = &pv
						break
					}
				}

				if foundPV == nil {
					// if there was no PV that matched it and it still is flagged as LVM2_member, it is formatted but not recognized by LVM
					// configuration. We can assume that in this case, the Volume can be reused by simply recalling the vgcreate command on it
					return nil
				}

				// a volume is a valid PV if it exists under the same name as the Block Device and has either
				// 1. No Children, No Volume Group attached to it and Free Capacity (then we can reuse it)
				if !dev.HasChildren() {
					if foundPV.VgName != "" {
						return fmt.Errorf("%s is already part of another volume group (%s) and cannot be used", dev.Name, foundPV.VgName)
					}
					if foundPV.PvFree == "0G" {
						return fmt.Errorf("%s was reported as having no free capacity as a physical volume and cannot be used", dev.Name)
					}
					return nil
				}

				// 2. Children and has a volume group that matches the one we want to filter for (it is already used)
				if foundPV.VgName != vg.GetName() {
					return fmt.Errorf("%s is already a LVM2_Member of another volume group (%s) and cannot be used for the volume group %s",
						dev.Name, foundPV.VgName, vg.GetName())
				} else {
					return fmt.Errorf("%s is already a LVM2_Member of %s: %w", dev.Name, vg.GetName(), ErrDeviceAlreadySetupCorrectly)
				}
			}
			return fmt.Errorf("%s has an invalid filesystem signature (%s) and cannot be used", dev.Name, dev.FSType)
		},

		noChildren: func(dev lsblk.BlockDevice) error {
			hasChildren := dev.HasChildren()
			if hasChildren {
				return fmt.Errorf("%s has children block devices and could not be considered", dev.Name)
			}
			return nil
		},

		noBindMounts: func(dev lsblk.BlockDevice) error {
			hasBindMounts, _, err := lsblkInstance.HasBindMounts(dev)
			if err != nil {
				return fmt.Errorf("could not determine if %s had bind mounts: %w", dev.Name, err)
			}
			if hasBindMounts {
				return fmt.Errorf("%s has bind mounts and cannot be used", dev.Name)
			}
			return nil
		},

		usableDeviceType: func(dev lsblk.BlockDevice) error {
			switch dev.Type {
			case DeviceTypeLoop:
				// check loop device isn't being used by kubernetes
				usable, err := lsblkInstance.IsUsableLoopDev(dev)
				if err != nil {
					return fmt.Errorf("%s is a loopback device that could not be verified as usable: %w", dev.Name, err)
				}
				if !usable {
					return fmt.Errorf("%s is an unusable loopback device: %w", dev.Name, err)
				}
			case DeviceTypeROM:
				return fmt.Errorf("%s has a device type of %q which is unsupported", dev.Name, dev.Type)
			case DeviceTypeLVM:
				return ErrLVMPartition
			}
			return nil
		},
	}
}
