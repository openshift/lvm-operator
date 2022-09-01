/*
Copyright 2021 Red Hat Openshift Data Foundation.

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
	"strings"

	"github.com/red-hat-storage/lvm-operator/pkg/internal"
)

const (
	// filter names:
	notReadOnly           = "notReadOnly"
	notSuspended          = "notSuspended"
	noBiosBootInPartLabel = "noBiosBootInPartLabel"
	noFilesystemSignature = "noFilesystemSignature"
	noBindMounts          = "noBindMounts"
	noChildren            = "noChildren"
	usableDeviceType      = "usableDeviceType"
)

// maps of function identifier (for logs) to filter function.
// These are passed the localv1alpha1.DeviceInclusionSpec to make testing easier,
// but they aren't expected to use it
// they verify that the device itself is good to use
var FilterMap = map[string]func(internal.BlockDevice, internal.Executor) (bool, error){
	notReadOnly: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		readOnly, err := dev.IsReadOnly()
		return !readOnly, err
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

	noFilesystemSignature: func(dev internal.BlockDevice, _ internal.Executor) (bool, error) {
		matched := dev.FSType == ""
		return matched, nil
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
		default:
			return true, nil
		}
	},
}
