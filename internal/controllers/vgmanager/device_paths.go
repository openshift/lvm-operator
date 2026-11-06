/*
Copyright © 2026 Red Hat, Inc.

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
	"slices"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
)

// resolveSelectedDevices expands a selector entry into the devices to put in the volume
// group. When a glob matches both a device and one of its own partitions, only the
// partition is kept: a device with children is never a valid physical volume, so the
// innermost match is the one the filter chain can actually use.
func resolveSelectedDevices(path v1alpha1.DevicePath, devices []lsblk.BlockDevice, resolver *symlinkResolver.Resolver) ([]string, error) {
	return resolveDevicePaths(path, devices, resolver, false)
}

// resolveWipeTargets expands a selector entry into the devices to wipe. Here the
// outermost match wins: wiping a disk already takes its partitions with it, so wiping
// both would operate on a device that no longer exists.
func resolveWipeTargets(path v1alpha1.DevicePath, devices []lsblk.BlockDevice, resolver *symlinkResolver.Resolver) ([]string, error) {
	return resolveDevicePaths(path, devices, resolver, true)
}

// resolveDevicePaths expands a selector against kernel device names. Literal paths
// retain their existing symlink resolution behavior, even when absent from the tree.
func resolveDevicePaths(path v1alpha1.DevicePath, devices []lsblk.BlockDevice, resolver *symlinkResolver.Resolver, keepOutermost bool) ([]string, error) {
	if !path.IsGlob() {
		resolved, err := resolver.Resolve(path.Unresolved())
		if err != nil {
			return nil, err
		}
		return []string{resolved}, nil
	}

	// Check the pattern up front, not only as a side effect of matching: an empty device
	// tree would otherwise report "matched nothing" for a pattern that cannot ever match.
	if err := path.Validate(); err != nil {
		return nil, err
	}

	var matched []string
	// walk reports whether anything in the subtree matched, so a parent can tell
	// whether it is the innermost match.
	var walk func([]lsblk.BlockDevice) (bool, error)
	walk = func(devices []lsblk.BlockDevice) (bool, error) {
		found := false
		for _, device := range devices {
			hit, err := path.Match(device.KName)
			if err != nil {
				return false, err
			}
			if hit && keepOutermost {
				matched = append(matched, device.KName)
				found = true
				continue
			}
			childHit, err := walk(device.Children)
			if err != nil {
				return false, err
			}
			if hit && !childHit {
				matched = append(matched, device.KName)
			}
			found = found || hit || childHit
		}
		return found, nil
	}
	if _, err := walk(devices); err != nil {
		return nil, err
	}

	slices.Sort(matched)
	return slices.Compact(matched), nil
}

// globPaths returns the pattern entries of a selector, required and optional together.
func globPaths(selector *v1alpha1.DeviceSelector) []v1alpha1.DevicePath {
	if selector == nil {
		return nil
	}
	var globs []v1alpha1.DevicePath
	for _, paths := range [][]v1alpha1.DevicePath{selector.Paths, selector.OptionalPaths} {
		for _, path := range paths {
			if path.IsGlob() {
				globs = append(globs, path)
			}
		}
	}
	return globs
}

// resolveForGlobMatching returns the name to match the patterns against. A name a pattern
// already matches is returned as-is, so the common case costs no symlink lookup and
// cannot be broken by a transient one.
func resolveForGlobMatching(name string, globs []v1alpha1.DevicePath, resolver *symlinkResolver.Resolver) (string, error) {
	for _, glob := range globs {
		matched, err := glob.Match(name)
		if err != nil {
			return "", err
		}
		if matched {
			return name, nil
		}
	}
	resolved, err := resolver.Resolve(name)
	if err != nil {
		return "", fmt.Errorf("failed to resolve PV %s to the kernel name that %v are matched against: %w", name, globs, err)
	}
	return resolved, nil
}

// knownDevices wraps bare kernel names as a flat device tree, for callers that already
// resolved the devices themselves (for example the physical volumes of an existing VG).
func knownDevices(knames []string) []lsblk.BlockDevice {
	devices := make([]lsblk.BlockDevice, 0, len(knames))
	for _, kname := range knames {
		devices = append(devices, lsblk.BlockDevice{KName: kname})
	}
	return devices
}

// validateDeviceSelector rejects selectors the node must not act on. It repeats the
// LVMCluster webhook's checks because LVMVolumeGroups can be created by hand, which
// bypasses admission entirely. Run before any LVM changes.
func validateDeviceSelector(spec *v1alpha1.LVMVolumeGroupSpec) error {
	if err := validateDevicePaths(spec.DeviceSelector); err != nil {
		return err
	}
	if spec.RAIDConfig != nil && spec.DeviceSelector.HasGlobPaths() {
		return v1alpha1.ErrRAIDGlobPathsUnsupported
	}
	return nil
}

// validateDevicePaths checks pattern syntax only. Kept separate from
// validateDeviceSelector so the wipe path can re-check it without a full spec.
func validateDevicePaths(selector *v1alpha1.DeviceSelector) error {
	if selector == nil {
		return nil
	}
	for _, paths := range [][]v1alpha1.DevicePath{selector.Paths, selector.OptionalPaths} {
		for _, path := range paths {
			if err := path.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

func requireDevicePathMatches(path v1alpha1.DevicePath, matched []string) error {
	if len(matched) == 0 {
		return fmt.Errorf("mandatory device path %q did not match any kernel block devices", path)
	}
	return nil
}
