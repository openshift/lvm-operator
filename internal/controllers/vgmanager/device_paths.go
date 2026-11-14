package vgmanager

import (
	"fmt"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
)

// reduceGlobMatches removes matched ancestors or descendants using the lsblk
// tree. Only actual block devices can be selected by a glob.
func reduceGlobMatches(paths []string, devices []lsblk.BlockDevice, keepOutermost bool) []string {
	selected := make(map[string]bool, len(paths))
	for _, path := range paths {
		selected[path] = true
	}
	seen := make(map[string]bool)
	suppressed := make(map[string]bool)
	var walk func(lsblk.BlockDevice, bool) bool
	walk = func(device lsblk.BlockDevice, ancestorMatched bool) bool {
		hit := selected[device.KName]
		seen[device.KName] = true
		childMatched := false
		for _, child := range device.Children {
			if walk(child, ancestorMatched || hit) {
				childMatched = true
			}
		}
		if hit && ((keepOutermost && ancestorMatched) || (!keepOutermost && childMatched)) {
			suppressed[device.KName] = true
		}
		return hit || childMatched
	}
	for _, device := range devices {
		walk(device, false)
	}
	var result []string
	for _, path := range paths {
		if seen[path] && !suppressed[path] {
			result = append(result, path)
		}
	}
	return result
}

func validateDeviceSelector(spec *v1alpha1.LVMVolumeGroupSpec) error {
	if spec.DeviceSelector == nil {
		return nil
	}
	if spec.RAIDConfig != nil && spec.DeviceSelector.HasGlobPaths() {
		return v1alpha1.ErrRAIDGlobPathsUnsupported
	}
	for _, paths := range [][]v1alpha1.DevicePath{spec.DeviceSelector.Paths, spec.DeviceSelector.OptionalPaths} {
		for _, path := range paths {
			if err := path.Validate(); err != nil {
				return fmt.Errorf("invalid device selector: %w", err)
			}
		}
	}
	return nil
}
