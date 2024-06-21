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
	"path/filepath"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
)

const (
	// StateSuspended is a possible value of BlockDevice.State
	StateSuspended = "suspended"
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

// evalSymlinks redefined to be able to override in tests
var evalSymlinks = filepath.EvalSymlinks

type Filter func(lsblk.BlockDevice, []lvm.PhysicalVolume, lsblk.BlockDeviceInfos) error

type Filters map[string]Filter

// IsExpectedDeviceErrorAfterSetup can be used to check for errors in the filtering process that are expected after
// the volume group has been initialized correctly because the devices for the volume group create new devices or
// change their attributes.
func IsExpectedDeviceErrorAfterSetup(err error) bool {
	return errors.Is(err, ErrDeviceAlreadySetupCorrectly) || errors.Is(err, ErrLVMPartition)
}

func DefaultFilters(vg *lvmv1alpha1.LVMVolumeGroup) Filters {
	return Filters{
		notReadOnly: func(dev lsblk.BlockDevice, _ []lvm.PhysicalVolume, _ lsblk.BlockDeviceInfos) error {
			if dev.ReadOnly {
				return fmt.Errorf("%s cannot be read-only", dev.Name)
			}
			return nil
		},

		notSuspended: func(dev lsblk.BlockDevice, _ []lvm.PhysicalVolume, _ lsblk.BlockDeviceInfos) error {
			if dev.State == StateSuspended {
				return fmt.Errorf("%s cannot be in a %q state", dev.State, dev.Name)
			}
			return nil
		},

		noInvalidPartitionLabel: func(dev lsblk.BlockDevice, _ []lvm.PhysicalVolume, _ lsblk.BlockDeviceInfos) error {
			for _, invalidLabel := range invalidPartitionLabels {
				if strings.Contains(strings.ToLower(dev.PartLabel), invalidLabel) {
					return fmt.Errorf("%s has an invalid partition label %q", dev.Name, dev.PartLabel)
				}
			}
			return nil
		},

		onlyValidFilesystemSignatures: func(dev lsblk.BlockDevice, pvs []lvm.PhysicalVolume, _ lsblk.BlockDeviceInfos) error {
			// if no fs type is set, it's always okay
			if dev.FSType == "" {
				return nil
			}

			// if fstype is set to LVM2_member then it already was created as a PV
			// this means that if the disk has no children, we can safely reuse it if it's a valid LVM PV.
			if dev.FSType == FSTypeLVM2Member {
				var foundPV *lvm.PhysicalVolume
				for _, pv := range pvs {
					resolvedPVPath, err := evalSymlinks(pv.PvName)
					if err != nil {
						return fmt.Errorf("the pv %s could not be resolved via symlink: %w", pv, err)
					}
					if resolvedPVPath == dev.KName {
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

		noChildren: func(dev lsblk.BlockDevice, _ []lvm.PhysicalVolume, _ lsblk.BlockDeviceInfos) error {
			hasChildren := dev.HasChildren()
			if hasChildren {
				return fmt.Errorf("%s has children block devices and could not be considered", dev.Name)
			}
			return nil
		},

		usableDeviceType: func(dev lsblk.BlockDevice, _ []lvm.PhysicalVolume, bdi lsblk.BlockDeviceInfos) error {
			switch dev.Type {
			case lsblk.DeviceTypeLoop:
				// check loop device isn't being used by kubernetes
				if !bdi[dev.KName].IsUsableLoopDev {
					return fmt.Errorf("%s is an unusable loopback device", dev.Name)
				}
			case lsblk.DeviceTypeROM:
				return fmt.Errorf("%s has a device type of %q which is unsupported", dev.Name, dev.Type)
			case lsblk.DeviceTypeLVM:
				return ErrLVMPartition
			}
			return nil
		},
	}
}
