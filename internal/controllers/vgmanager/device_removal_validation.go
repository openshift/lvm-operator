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
)

// validateDeviceRemoval checks if a device can be safely removed from the volume group.
// This function is used by the transaction system for validation.
func (r *Reconciler) validateDeviceRemoval(ctx context.Context, devicePath, vgName string) error {
	// Check if device has allocated extents
	hasAllocatedExtents, err := r.HasAllocatedExtents(ctx, devicePath)
	if err != nil {
		return fmt.Errorf("failed to check if device %s has allocated extents: %w", devicePath, err)
	}

	if hasAllocatedExtents {
		return fmt.Errorf("device %s has allocated logical volume extents and cannot be safely removed", devicePath)
	}

	// Ensure VG remains viable after removal
	vg, err := r.GetVG(ctx, vgName)
	if err != nil {
		return fmt.Errorf("failed to get volume group info for %s: %w", vgName, err)
	}

	if len(vg.PVs) <= 1 {
		return fmt.Errorf("cannot remove the last device from volume group %s", vgName)
	}

	return nil
}
