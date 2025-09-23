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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	deviceMappingConfigMapPrefix = "lvm-device-mappings"
)

// storeDevicePathMappings stores the provided mapping between user-provided paths and actual resolved paths
// in a ConfigMap for the current node and VG. This enables device removal even when symlinks become invalid.
func (r *Reconciler) storeDevicePathMappings(ctx context.Context, vgName string, pathMappings map[string]string) error {
	logger := log.FromContext(ctx).WithValues("VGName", vgName)

	if len(pathMappings) == 0 {
		logger.Info("no device path mappings to store")
		return nil
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", deviceMappingConfigMapPrefix, r.NodeName),
			Namespace: r.Namespace,
		},
		Data: make(map[string]string),
	}

	// Convert path mappings to string format for ConfigMap storage
	var mappings []string
	for originalPath, resolvedPath := range pathMappings {
		mappings = append(mappings, fmt.Sprintf("%s=%s", originalPath, resolvedPath))
	}

	configMap.Data[vgName] = strings.Join(mappings, "\n")

	_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Data[vgName] = strings.Join(mappings, "\n")
		return nil
	})

	return err
}

// getStoredDevicePathMappings retrieves the stored path mappings for a VG
func (r *Reconciler) getStoredDevicePathMappings(ctx context.Context, vgName string) (map[string]string, error) {
	configMapName := fmt.Sprintf("%s-%s", deviceMappingConfigMapPrefix, r.NodeName)
	configMap := &corev1.ConfigMap{}

	err := r.Get(ctx, types.NamespacedName{
		Name:      configMapName,
		Namespace: r.Namespace,
	}, configMap)

	if err != nil {
		return nil, fmt.Errorf("failed to get device mapping %s ConfigMap: %w", configMapName, err)
	}

	data, exists := configMap.Data[vgName]
	if !exists {
		return nil, fmt.Errorf("failed to get device mapping for %s volume group", vgName)
	}

	mappings := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			mappings[parts[0]] = parts[1]
		}
	}

	return mappings, nil
}

// removeDevicePathMapping removes a specific device mapping from the ConfigMap
func (r *Reconciler) removeDevicePathMapping(ctx context.Context, vgName, originalPath string) error {
	logger := log.FromContext(ctx).WithValues("VGName", vgName, "originalPath", originalPath)

	configMapName := fmt.Sprintf("%s-%s", deviceMappingConfigMapPrefix, r.NodeName)
	configMap := &corev1.ConfigMap{}

	err := r.Get(ctx, types.NamespacedName{
		Name:      configMapName,
		Namespace: r.Namespace,
	}, configMap)

	if err != nil {
		return fmt.Errorf("failed to get device mapping %s ConfigMap: %w", configMapName, err)
	}

	data, exists := configMap.Data[vgName]
	if !exists {
		return fmt.Errorf("failed to get device mapping for %s volume group", vgName)

	}

	var remainingMappings []string
	for _, line := range strings.Split(data, "\n") {
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			if parts[0] != originalPath {
				remainingMappings = append(remainingMappings, line)
			}
		}
	}

	if len(remainingMappings) > 0 {
		configMap.Data[vgName] = strings.Join(remainingMappings, "\n")
	} else {
		delete(configMap.Data, vgName)
	}

	err = r.Update(ctx, configMap)
	if err != nil {
		return fmt.Errorf("failed to update device mapping ConfigMap: %w", err)
	}

	logger.Info("removed device path mapping", "originalPath", originalPath)

	return nil
}
