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
	"fmt"
	"sync"

	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// DeviceRemovalTransaction represents a transactional device removal operation
type DeviceRemovalTransaction struct {
	vgName         string
	namespace      string
	devicePaths    []string
	operations     []deviceOperation
	reconciler     *Reconciler
	initialVGState lvm.VolumeGroup
	mutex          sync.Mutex
	executed       bool
}

type deviceOperation struct {
	devicePath   string
	originalPath string // The user-provided path (symlink) corresponding to devicePath
	phase        operationPhase
	rollback     func(ctx context.Context) error
}

type operationPhase int

const (
	phaseValidation operationPhase = iota
	phaseVGReduce
	phasePVRemove
	phaseCompleted
)

// NewDeviceRemovalTransaction creates a new transactional device removal operation
func (r *Reconciler) NewDeviceRemovalTransaction(vgName, namespace string, devicePaths []string) *DeviceRemovalTransaction {
	return &DeviceRemovalTransaction{
		vgName:      vgName,
		namespace:   namespace,
		devicePaths: devicePaths,
		operations:  make([]deviceOperation, 0, len(devicePaths)),
		reconciler:  r,
	}
}

// Execute performs the transactional device removal with automatic rollback on failure
func (tx *DeviceRemovalTransaction) Execute(ctx context.Context) error {
	// Protect against concurrent execution and double execution
	tx.mutex.Lock()
	defer tx.mutex.Unlock()

	if tx.executed {
		return fmt.Errorf("transaction has already been executed")
	}
	tx.executed = true

	logger := log.FromContext(ctx).WithValues("VGName", tx.vgName)
	logger.Info("starting transactional device removal", "deviceCount", len(tx.devicePaths))

	if len(tx.devicePaths) == 0 {
		logger.Info("no devices to remove")
		return nil
	}

	// Phase 0: Capture initial VG state for rollback
	logger.Info("phase 0: capturing initial VG state for rollback")
	initialVG, err := tx.reconciler.GetVG(ctx, tx.vgName)
	if err != nil {
		return fmt.Errorf("failed to capture initial VG state: %w", err)
	}
	tx.initialVGState = initialVG

	// Phase 1: Validate all devices (prepare phase)
	logger.Info("phase 1: validating all devices for removal")
	for _, devicePath := range tx.devicePaths {
		if err := tx.reconciler.validateDeviceRemoval(ctx, devicePath, tx.vgName); err != nil {
			return fmt.Errorf("prepare phase validation failed for device %s: %w", devicePath, err)
		}
	}
	logger.Info("phase 1 completed: all devices validated successfully")

	// Phase 2: Execute operations with rollback tracking (commit phase)
	logger.Info("phase 2: executing device removal operations")
	for i, devicePath := range tx.devicePaths {
		logger.Info("processing device", "device", devicePath, "progress", fmt.Sprintf("%d/%d", i+1, len(tx.devicePaths)))
		if err := tx.executeDeviceRemoval(ctx, devicePath); err != nil {
			logger.Error(err, "device removal failed, initiating rollback", "device", devicePath)
			// Rollback all completed operations
			if rollbackErr := tx.rollback(ctx); rollbackErr != nil {
				return fmt.Errorf("operation failed and rollback failed: %w, rollback error: %v", err, rollbackErr)
			}
			return fmt.Errorf("operation failed and was rolled back: %w", err)
		}
	}

	logger.Info("transactional device removal completed successfully", "devicesRemoved", len(tx.devicePaths))
	return nil
}

// executeDeviceRemoval performs removal of a single device with rollback tracking
func (tx *DeviceRemovalTransaction) executeDeviceRemoval(ctx context.Context, devicePath string) error {
	logger := log.FromContext(ctx).WithValues("device", devicePath)

	op := deviceOperation{
		devicePath: devicePath,
		phase:      phaseVGReduce,
	}

	if err := tx.reconciler.ReduceVG(ctx, tx.vgName, devicePath); err != nil {
		return fmt.Errorf("failed to reduce VG: %w", err)
	}

	// Set rollback function for VG reduce - use initial VG state that includes the device
	op.rollback = func(ctx context.Context) error {
		logger.Info("rolling back VG reduce operation")
		// Use initial VG state which contains all devices before any removal
		_, err := tx.reconciler.ExtendVG(ctx, tx.initialVGState, []string{devicePath})
		if err != nil {
			return fmt.Errorf("failed to rollback VG reduce: %w", err)
		}
		return nil
	}

	logger.Info("VG reduce completed, rollback prepared")

	// Step 3: PV Remove
	op.phase = phasePVRemove
	if err := tx.reconciler.RemovePV(ctx, devicePath); err != nil {
		// Add operation to list for rollback before returning error
		tx.operations = append(tx.operations, op)
		return fmt.Errorf("failed to remove PV: %w", err)
	}

	op.phase = phaseCompleted
	// For completed operations, we need a different rollback that restores the full device
	op.rollback = func(ctx context.Context) error {
		logger.Info("rolling back completed device removal")
		// For a completed removal, we need to add the device back to the VG
		// Use initial VG state which contains all the original devices
		_, err := tx.reconciler.ExtendVG(ctx, tx.initialVGState, []string{devicePath})
		if err != nil {
			return fmt.Errorf("failed to rollback completed device removal: %w", err)
		}
		return nil
	}
	tx.operations = append(tx.operations, op)

	logger.Info("device removal completed successfully")

	// Clean up path mapping for the removed device
	if err := tx.cleanupDevicePathMapping(ctx, devicePath); err != nil {
		logger.Error(err, "failed to cleanup device path mapping", "device", devicePath)
		// Don't fail the operation - device removal was successful
	}

	return nil
}

// rollback attempts to undo all completed operations
func (tx *DeviceRemovalTransaction) rollback(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("VGName", tx.vgName)
	logger.Info("starting rollback of device removal operations")

	var rollbackErrors []error

	// Rollback in reverse order
	for i := len(tx.operations) - 1; i >= 0; i-- {
		op := tx.operations[i]
		if op.rollback != nil {
			logger.Info("rolling back operation", "device", op.devicePath, "phase", op.phase)
			if err := op.rollback(ctx); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("failed to rollback device %s: %w", op.devicePath, err))
			}
		}
	}

	if len(rollbackErrors) > 0 {
		return fmt.Errorf("rollback had %d errors: %v", len(rollbackErrors), rollbackErrors)
	}

	logger.Info("rollback completed successfully")
	return nil
}

// cleanupDevicePathMapping removes the mapping for a successfully removed device
func (tx *DeviceRemovalTransaction) cleanupDevicePathMapping(ctx context.Context, devicePath string) error {
	logger := log.FromContext(ctx).WithValues("device", devicePath, "vg", tx.vgName)

	// Get stored mappings to find the original path for this device
	storedMappings, err := tx.reconciler.getStoredDevicePathMappings(ctx, tx.vgName)
	if err != nil {
		return fmt.Errorf("failed to get stored mappings for cleanup: %w", err)
	}

	// Find the original path that maps to this device
	var originalPath string
	for orig, resolved := range storedMappings {
		if resolved == devicePath {
			originalPath = orig
			break
		}
	}

	if originalPath == "" {
		logger.V(3).Info("no stored mapping found for device, skipping cleanup")
		return nil
	}

	// Remove the mapping
	if err := tx.reconciler.removeDevicePathMapping(ctx, tx.vgName, originalPath); err != nil {
		return fmt.Errorf("failed to remove device path mapping: %w", err)
	}

	logger.Info("cleaned up device path mapping", "originalPath", originalPath, "devicePath", devicePath)
	return nil
}
