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

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// buildDevicePathMappings creates a mapping from user-provided paths to resolved device paths
// for devices that are actually in the VG (using VG state from ListVGs)
func (r *Reconciler) buildDevicePathMappings(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup, vgs []lvm.VolumeGroup, resolver *symlinkResolver.Resolver) (map[string]string, error) {
	logger := log.FromContext(ctx).WithValues("VGName", volumeGroup.Name)

	if volumeGroup.Spec.DeviceSelector == nil {
		return nil, nil
	}

	// Find our VG and get its actual devices
	acutalVGDevices := make(map[string]struct{})
	for _, vg := range vgs {
		if vg.Name == volumeGroup.Name {
			for _, pv := range vg.PVs {
				acutalVGDevices[pv.PvName] = struct{}{}
			}
			break
		}
	}

	if len(acutalVGDevices) == 0 {
		logger.V(3).Info("VG not found in ListVGs output, skipping mapping creation")
		return nil, nil
	}

	mappings := make(map[string]string)

	// Process required paths
	for _, path := range volumeGroup.Spec.DeviceSelector.Paths {
		originalPath := path.Unresolved()
		resolved, err := resolver.Resolve(originalPath)
		if err != nil {
			return nil, err
		}

		// Only store mapping if this device is actually in the VG
		if _, ok := acutalVGDevices[resolved]; ok {
			mappings[originalPath] = resolved
		}
	}

	// Process optional paths
	for _, path := range volumeGroup.Spec.DeviceSelector.OptionalPaths {
		originalPath := path.Unresolved()
		resolved, err := resolver.Resolve(originalPath)
		if err != nil {
			logger.V(3).Info("failed to resolve optional device path during mapping build", "path", originalPath, "error", err)
			continue
		}

		// Only store mapping if this device is actually in the VG
		if _, ok := acutalVGDevices[resolved]; ok {
			mappings[originalPath] = resolved
		}
	}

	return mappings, nil
}
