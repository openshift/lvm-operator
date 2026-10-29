/*
Copyright © 2025 Red Hat, Inc.

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
	"slices"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// buildDevicePathMappings creates a mapping from user-provided paths to resolved device paths
// for devices that are actually in the VG (using VG state from ListVGs)
func buildDevicePathMappings(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup, resolver *symlinkResolver.Resolver, pvs ...lvm.PhysicalVolume) ([]string, error) {
	logger := log.FromContext(ctx).WithValues("VGName", volumeGroup.Name)

	if volumeGroup.Spec.DeviceSelector == nil {
		return nil, nil
	}

	resolvedPaths := make([]string, 0)
	// aliasDirs are the directories of alias patterns such as /dev/disk/by-id/*.
	// resolutionIncomplete means a path could not be expanded, so no conclusion
	// about which PVs are still selected can be trusted at all.
	var aliasDirs []string
	resolutionIncomplete := false

	// Process required paths
	for _, path := range volumeGroup.Spec.DeviceSelector.Paths {
		aliasDirs = addAliasDir(aliasDirs, path)
		originalPath := path.Unresolved()
		resolved, err := resolver.ResolvePaths(originalPath)
		if err != nil && (!path.IsGlob() || len(resolved) == 0) {
			return nil, fmt.Errorf("failed to resolve path %s, %w", originalPath, err)
		}
		if err != nil {
			// An unresolved alias might have selected another existing PV.
			// Do not remove any PV that cannot be matched below.
			resolutionIncomplete = true
		}
		if path.IsGlob() {
			matchedPVs, err := matchPVsByGlob(path, pvs, resolver)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, matchedPVs...)
		}

		resolvedPaths = append(resolvedPaths, resolved...)
	}

	// Process optional paths
	for _, path := range volumeGroup.Spec.DeviceSelector.OptionalPaths {
		originalPath := path.Unresolved()
		resolved, err := resolver.ResolvePaths(originalPath)
		if path.IsGlob() {
			aliasDirs = addAliasDir(aliasDirs, path)
			// Compare existing PVs directly so a transiently missing /dev entry
			// cannot turn a still-selected kernel device into a removal request.
			matchedPVs, matchErr := matchPVsByGlob(path, pvs, resolver)
			if matchErr != nil {
				return nil, matchErr
			}
			resolved = append(resolved, matchedPVs...)
			if err != nil && !errors.Is(err, symlinkResolver.ErrGlobNoMatches) {
				if len(resolved) == 0 {
					return nil, fmt.Errorf("cannot safely evaluate optional glob %s: %w", path, err)
				}
				resolutionIncomplete = true
			}
		}
		if err != nil {
			logger.Info("failed to resolve optional device path during mapping build", "path", originalPath, "error", err)
		}

		resolvedPaths = append(resolvedPaths, resolved...)
	}

	// Filesystem glob expansion cannot prove that a missing alias never selected
	// an existing PV. Do not turn incomplete alias discovery into VG reduction.
	// A PV the alias directories still describe is genuinely deselected, though:
	// treating every unmatched PV as suspect made narrowing an alias pattern
	// impossible, because the devices the admin dropped are exactly the ones that
	// stop matching.
	if resolutionIncomplete || len(aliasDirs) > 0 {
		// Only scanned once a PV actually looks deselected, which is rare: the usual
		// reconcile matches every PV and must not pay for reading the alias directory.
		var aliased map[string]bool
		for _, pv := range pvs {
			if slices.Contains(resolvedPaths, pv.PvName) {
				continue
			}
			resolved, err := resolver.Resolve(pv.PvName)
			if err != nil {
				return nil, fmt.Errorf("cannot determine whether PV %s is still selected: %w", pv.PvName, err)
			}
			if slices.Contains(resolvedPaths, resolved) {
				continue
			}
			if resolutionIncomplete {
				return nil, fmt.Errorf("cannot determine whether PV %s is still selected: "+
					"a device path could not be expanded on this pass", pv.PvName)
			}
			if aliased == nil {
				aliased = aliasedDevices(aliasDirs, resolver)
			}
			if !aliased[resolved] {
				return nil, fmt.Errorf("cannot determine whether PV %s is still selected: "+
					"no alias under %s resolves to it, so one may not have appeared yet",
					pv.PvName, strings.Join(aliasDirs, ", "))
			}
		}
	}

	return resolvedPaths, nil
}

// addAliasDir records the directory of an alias pattern such as /dev/disk/by-id/*.
// Patterns directly under /dev name kernel devices and need no alias reasoning.
func addAliasDir(dirs []string, path lvmv1alpha1.DevicePath) []string {
	pattern, glob := path.GlobPattern()
	if !glob || !strings.Contains(strings.TrimPrefix(pattern, "/dev/"), "/") {
		return dirs
	}
	dir := filepath.Dir(pattern)
	if slices.Contains(dirs, dir) {
		return dirs
	}
	return append(dirs, dir)
}

// aliasedDevices returns the kernel devices the alias directories currently point
// at. A PV found here has a live alias, so a pattern that does not match it has
// deselected it on purpose rather than lost a race with udev.
func aliasedDevices(dirs []string, resolver *symlinkResolver.Resolver) map[string]bool {
	devices := make(map[string]bool)
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*"))
		if err != nil {
			continue
		}
		for _, match := range matches {
			if resolved, err := resolver.Resolve(match); err == nil {
				devices[resolved] = true
			}
		}
	}
	return devices
}

func matchPVsByGlob(path lvmv1alpha1.DevicePath, pvs []lvm.PhysicalVolume, resolver *symlinkResolver.Resolver) ([]string, error) {
	var matchedPaths []string
	for _, pv := range pvs {
		matched, err := path.Match(pv.PvName)
		if err != nil {
			return nil, err
		}
		if matched {
			matchedPaths = append(matchedPaths, pv.PvName)
			continue
		}
		pvPath, err := resolver.Resolve(pv.PvName)
		if err != nil {
			return nil, fmt.Errorf("cannot verify whether PV %s matches %s: %w", pv.PvName, path, err)
		}
		matched, err = path.Match(pvPath)
		if err != nil {
			return nil, err
		}
		if matched {
			matchedPaths = append(matchedPaths, pvPath)
		}
	}
	return matchedPaths, nil
}
