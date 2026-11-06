package vgmanager

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type globWipeCandidate struct {
	device   lsblk.BlockDevice
	selector v1alpha1.DevicePath
}

// wipeGlobDevices validates the full target set before recording the one-shot
// marker. Every target is then rediscovered and checked again immediately before
// wiping. The marker is persisted first so retries cannot wipe a device twice.
func (r *Reconciler) wipeGlobDevices(ctx context.Context, vg *v1alpha1.LVMVolumeGroup, devices []lsblk.BlockDevice, pvs []lvm.PhysicalVolume, protected map[string][]string, resolver *symlinkResolver.Resolver) (bool, error) {
	logger := log.FromContext(ctx)
	for _, pv := range pvs {
		if pv.VgName == vg.Name {
			return false, nil
		}
	}
	var candidates []globWipeCandidate
	seen := make(map[string]bool)
	selector := vg.Spec.DeviceSelector
	for _, group := range []struct {
		paths    []v1alpha1.DevicePath
		required bool
	}{{selector.Paths, true}, {selector.OptionalPaths, false}} {
		for _, path := range group.paths {
			resolved, err := resolver.ResolvePaths(path.Unresolved())
			if err != nil {
				if group.required {
					return false, fmt.Errorf("cannot resolve required device %s: %w", path, err)
				}
				logger.Info("skipping optional device selector", "path", path, "error", err)
				continue
			}
			if path.IsGlob() {
				resolved = reduceGlobMatches(resolved, devices, false)
			}
			eligible := 0
			for _, target := range resolved {
				device, err := globWipeDevice(target, devices, protected)
				if err != nil {
					if group.required && !path.IsGlob() {
						return false, fmt.Errorf("unsafe required device %s: %w", path, err)
					}
					logger.Info("skipping unsafe device", "path", path, "device", target, "reason", err)
					continue
				}
				eligible++
				if !seen[target] {
					seen[target] = true
					candidates = append(candidates, globWipeCandidate{device: device, selector: path})
				}
			}
			if group.required && eligible == 0 {
				return false, fmt.Errorf("required device selector %q has no safe devices to wipe", path)
			}
		}
	}
	if len(candidates) == 0 {
		return false, nil
	}

	if vg.Annotations == nil {
		vg.Annotations = make(map[string]string)
	}
	vg.Annotations[constants.DevicesWipedAnnotationPrefix+r.NodeName] = fmt.Sprintf("glob wipe started at %s; this marker prevents retries from wiping devices again", time.Now().Format(time.RFC3339))
	if err := r.Update(ctx, vg); err != nil {
		return false, fmt.Errorf("failed to persist wipe marker before wiping: %w", err)
	}

	wiped := false
	for _, candidate := range candidates {
		freshDevices, err := r.ListBlockDevices(ctx)
		if err != nil {
			return wiped, fmt.Errorf("failed to recheck block devices before wiping: %w", err)
		}
		freshPVs, err := r.ListPVs(ctx, "")
		if err != nil {
			return wiped, fmt.Errorf("failed to recheck PV ownership before wiping: %w", err)
		}
		freshResolver := resolver.Fresh()
		freshProtected, err := protectedDevicePaths(freshDevices, freshPVs, freshResolver, true)
		if err != nil {
			return wiped, err
		}
		fresh, err := globWipeDevice(candidate.device.KName, freshDevices, freshProtected)
		if err != nil {
			return wiped, fmt.Errorf("device %s is no longer safe to wipe: %w", candidate.device.KName, err)
		}
		if fresh.MajorMinor != candidate.device.MajorMinor || fresh.Serial != candidate.device.Serial || fresh.WWN != candidate.device.WWN || fresh.Size != candidate.device.Size {
			return wiped, fmt.Errorf("device %s changed identity before wiping", candidate.device.KName)
		}
		matches, err := freshResolver.ResolvePaths(candidate.selector.Unresolved())
		if err != nil || !slices.Contains(matches, candidate.device.KName) {
			return wiped, fmt.Errorf("device %s no longer matches selector %s: %v", candidate.device.KName, candidate.selector, err)
		}
		current := &v1alpha1.LVMVolumeGroup{}
		reader := r.APIReader
		if reader == nil {
			reader = r.Client
		}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(vg), current); err != nil {
			return wiped, fmt.Errorf("failed to recheck volume group before wiping: %w", err)
		}
		markerKey := constants.DevicesWipedAnnotationPrefix + r.NodeName
		if current.UID != vg.UID || !current.DeletionTimestamp.IsZero() || !reflect.DeepEqual(current.Spec, vg.Spec) || current.Annotations[markerKey] != vg.Annotations[markerKey] {
			return wiped, fmt.Errorf("volume group configuration or wipe marker changed before wiping %s", candidate.device.KName)
		}
		deviceWiped, err := r.wipeDevice(ctx, candidate.device.KName, freshDevices)
		if err != nil {
			return wiped, fmt.Errorf("failed to wipe device %s: %w", candidate.device.KName, err)
		}
		if !deviceWiped {
			return wiped, fmt.Errorf("device %s disappeared before wiping", candidate.device.KName)
		}
		wiped = true
	}
	return wiped, nil
}
