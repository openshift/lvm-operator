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
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// buildDevicePathMappings creates a mapping from user-provided paths to resolved device paths
// for devices that are actually in the VG (using VG state from ListVGs)
func buildDevicePathMappings(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup, resolver *symlinkResolver.Resolver) ([]string, error) {
	logger := log.FromContext(ctx).WithValues("VGName", volumeGroup.Name)

	if volumeGroup.Spec.DeviceSelector == nil {
		return nil, nil
	}

	resolvedPaths := make([]string, 0)

	// Process required paths
	for _, path := range volumeGroup.Spec.DeviceSelector.Paths {
		originalPath := path.Unresolved()
		resolved, err := resolver.Resolve(originalPath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve path %s, %w", originalPath, err)
		}

		resolvedPaths = append(resolvedPaths, resolved)
	}

	// Process optional paths
	for _, path := range volumeGroup.Spec.DeviceSelector.OptionalPaths {
		originalPath := path.Unresolved()
		resolved, err := resolver.Resolve(originalPath)
		if err != nil {
			logger.Info("failed to resolve optional device path during mapping build", "path", originalPath, "error", err)
			continue
		}

		resolvedPaths = append(resolvedPaths, resolved)
	}

	return resolvedPaths, nil
}
