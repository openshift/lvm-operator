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
	"context"
	"errors"
	"fmt"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"

	"sigs.k8s.io/controller-runtime/pkg/log"
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
	noChildren                    = "noChildren"
	usableDeviceType              = "usableDeviceType"
	partOfDeviceSelector          = "partOfDeviceSelector"
	staticDiscoveryPolicy         = "staticDiscoveryPolicy"
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

type Filter func(lsblk.BlockDevice, *symlinkResolver.Resolver) error

type Filters map[string]Filter

type Options struct {
	VG  *lvmv1alpha1.LVMVolumeGroup
	BDI lsblk.BlockDeviceInfos
	PVs []lvm.PhysicalVolume
	VGs []lvm.VolumeGroup
}

type FilterSetup func(context.Context, *Options) Filters

var _ FilterSetup = DefaultFilters

// IsExpectedDeviceErrorAfterSetup can be used to check for errors in the filtering process that are expected after
// the volume group has been initialized correctly because the devices for the volume group create new devices or
// change their attributes.
func IsExpectedDeviceErrorAfterSetup(err error) bool {
	return errors.Is(err, ErrDeviceAlreadySetupCorrectly) || errors.Is(err, ErrLVMPartition)
}

func DefaultFilters(ctx context.Context, opts *Options) Filters {
	logger := log.FromContext(ctx)
	return Filters{
		staticDiscoveryPolicy: func(dev lsblk.BlockDevice, resolver *symlinkResolver.Resolver) error {
			// DeviceDiscoveryPolicy is only relevant when no explicit device paths are configured.
			// When a DeviceSelector is present, devices are preconfigured and the policy is ignored.
			if opts.VG.Spec.DeviceSelector != nil {
				return nil
			}

			// In static discovery mode, if VG already exists, no new devices should be discovered
			if opts.VG.Spec.DeviceDiscoveryPolicy == lvmv1alpha1.DeviceDiscoveryPolicySpecStatic {
				for _, vg := range opts.VGs {
					if vg.Name == opts.VG.Name {
						return fmt.Errorf("static discovery policy: VG %s already exists, device discovery disabled", opts.VG.Name)
					}
				}
			}
			return nil
		},

		partOfDeviceSelector: func(dev lsblk.BlockDevice, resolver *symlinkResolver.Resolver) error {
			if opts.VG.Spec.DeviceSelector == nil {
				// if no device selector is set, its automatically a valid candidate
				return nil
			}
			for _, path := range append(
				opts.VG.Spec.DeviceSelector.Paths,
				opts.VG.Spec.DeviceSelector.OptionalPaths...,
			) {
				// used the non-resolved path, e.g. /dev/disk/by-id/xyz
				if resolved, err := resolver.Resolve(path.Unresolved()); resolved == dev.KName {
					return nil
				} else if err != nil {
					logger.Error(err, "the path was no kernel block device name and could not be resolved via symlink resolution", "path", path)
					continue
				}
			}
			return fmt.Errorf("%s is not part of the device selector or could not be resolved via symlink resolution", dev.Name)
		},

		notReadOnly: func(dev lsblk.BlockDevice, _ *symlinkResolver.Resolver) error {
			if dev.ReadOnly {
				return fmt.Errorf("%s cannot be read-only", dev.Name)
			}
			return nil
		},

		notSuspended: func(dev lsblk.BlockDevice, _ *symlinkResolver.Resolver) error {
			if dev.State == StateSuspended {
				return fmt.Errorf("%s cannot be in a %q state", dev.State, dev.Name)
			}
			return nil
		},

		noInvalidPartitionLabel: func(dev lsblk.BlockDevice, _ *symlinkResolver.Resolver) error {
			for _, invalidLabel := range invalidPartitionLabels {
				if strings.Contains(strings.ToLower(dev.PartLabel), invalidLabel) {
					return fmt.Errorf("%s has an invalid partition label %q", dev.Name, dev.PartLabel)
				}
			}
			return nil
		},

		onlyValidFilesystemSignatures: func(dev lsblk.BlockDevice, resolver *symlinkResolver.Resolver) error {
			// if no fs type is set, it's always okay
			if dev.FSType == "" {
				return nil
			}

			// if fstype is set to LVM2_member then it already was created as a PV
			// this means that if the disk has no children, we can safely reuse it if it's a valid LVM PV.
			if dev.FSType == FSTypeLVM2Member {
				var foundPV *lvm.PhysicalVolume
				for _, pv := range opts.PVs {
					resolvedPVPath, err := resolver.Resolve(pv.PvName)
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

				if foundPV.VgName == opts.VG.GetName() {
					return fmt.Errorf("%s is already a LVM2_Member of %s: %w", dev.Name, opts.VG.GetName(), ErrDeviceAlreadySetupCorrectly)
				} else if foundPV.VgName != "" {
					return fmt.Errorf("%s is already a LVM2_Member of another volume group (%s) and cannot be used for the volume group %s",
						dev.Name, foundPV.VgName, opts.VG.GetName())
				}

				// a volume is a valid PV if it exists under the same name as the Block Device and has
				// 1. No Children
				// 2. No Volume Group attached to it and
				// 3. Free Capacity (then we can reuse it)
				if !dev.HasChildren() {
					if foundPV.PvFree == "0G" {
						return fmt.Errorf("%s was reported as having no free capacity as a physical volume and cannot be used", dev.Name)
					}
					return nil
				}
			}

			return fmt.Errorf("%s has an invalid filesystem signature (%s) and cannot be used", dev.Name, dev.FSType)
		},

		noChildren: func(dev lsblk.BlockDevice, _ *symlinkResolver.Resolver) error {
			hasChildren := dev.HasChildren()
			if hasChildren {
				return fmt.Errorf("%s has children block devices and could not be considered", dev.Name)
			}
			return nil
		},

		usableDeviceType: func(dev lsblk.BlockDevice, _ *symlinkResolver.Resolver) error {
			switch dev.Type {
			case lsblk.DeviceTypeLoop:
				// check loop device isn't being used by kubernetes
				if !opts.BDI[dev.KName].IsUsableLoopDev {
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
