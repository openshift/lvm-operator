package vgmanager

import (
	"fmt"
	"strconv"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
)

func buildRAIDLVCreateOptions(rc *lvmv1alpha1.RAIDConfig) []string {
	opts := []string{"--type", string(rc.Type)}

	switch rc.Type {
	case lvmv1alpha1.RAIDTypeRAID1:
		opts = append(opts, "-m", strconv.Itoa(rc.EffectiveMirrors()))
	case lvmv1alpha1.RAIDTypeRAID10:
		opts = append(opts, "-m", strconv.Itoa(rc.EffectiveMirrors()))
		if rc.Stripes != nil {
			opts = append(opts, "--stripes", strconv.Itoa(*rc.Stripes))
		}
		if rc.StripeSize != nil {
			opts = append(opts, "--stripesize", fmt.Sprintf("%dk", rc.StripeSize.Value()/1024))
		}
	case lvmv1alpha1.RAIDTypeRAID4, lvmv1alpha1.RAIDTypeRAID5, lvmv1alpha1.RAIDTypeRAID6:
		if rc.Stripes != nil {
			opts = append(opts, "--stripes", strconv.Itoa(*rc.Stripes))
		}
		if rc.StripeSize != nil {
			opts = append(opts, "--stripesize", fmt.Sprintf("%dk", rc.StripeSize.Value()/1024))
		}
	}

	return opts
}

func validateRAIDDeviceCount(rc *lvmv1alpha1.RAIDConfig, deviceCount int) error {
	mirrors := rc.EffectiveMirrors()
	minDevices := rc.Type.MinDeviceCount(mirrors, rc.Stripes)

	if deviceCount < minDevices {
		return fmt.Errorf("%s requires at least %d devices, got %d", rc.Type, minDevices, deviceCount)
	}

	if rc.Type == lvmv1alpha1.RAIDTypeRAID10 {
		groupSize := mirrors + 1
		if deviceCount%groupSize != 0 {
			if mirrors == 1 {
				return fmt.Errorf("raid10 with mirrors=1 requires an even number of devices, got %d", deviceCount)
			}
			return fmt.Errorf("raid10 with mirrors=%d requires a device count divisible by %d, got %d",
				mirrors, groupSize, deviceCount)
		}
	}

	return nil
}

// computeOverheadFactor returns the raw-to-usable space ratio for the given RAID level and device count.
func computeOverheadFactor(rc *lvmv1alpha1.RAIDConfig, deviceCount int) float64 {
	mirrors := rc.EffectiveMirrors()

	switch rc.Type {
	case lvmv1alpha1.RAIDTypeRAID1:
		return float64(mirrors + 1)
	case lvmv1alpha1.RAIDTypeRAID4, lvmv1alpha1.RAIDTypeRAID5:
		if rc.Stripes != nil {
			return float64(*rc.Stripes+1) / float64(*rc.Stripes)
		}
		if deviceCount <= 1 {
			return 1.0
		}
		return float64(deviceCount) / float64(deviceCount-1)
	case lvmv1alpha1.RAIDTypeRAID6:
		if rc.Stripes != nil {
			return float64(*rc.Stripes+2) / float64(*rc.Stripes)
		}
		if deviceCount <= 2 {
			return 1.0
		}
		return float64(deviceCount) / float64(deviceCount-2)
	case lvmv1alpha1.RAIDTypeRAID10:
		return float64(mirrors + 1)
	default:
		return 1.0
	}
}

// buildRAIDStatus inspects logical volumes and returns an aggregate RAIDStatus, or nil if no RAID LVs exist.
func buildRAIDStatus(lvs []lvm.LogicalVolume, raidType lvmv1alpha1.RAIDType) *lvmv1alpha1.RAIDStatus {
	var lvHealth []lvmv1alpha1.RAIDLVHealth

	for _, lv := range lvs {
		lvAttr, err := ParsedLvAttr(lv.LvAttr)
		if err != nil {
			continue
		}
		if lvAttr.VolumeType != VolumeTypeRAID && lvAttr.VolumeType != VolumeTypeRAIDNoInitialSync {
			continue
		}
		if strings.Contains(lv.Name, "_rimage_") || strings.Contains(lv.Name, "_rmeta_") {
			continue
		}

		syncPercent := 100
		if lv.RAIDSyncPercent != "" {
			if parsed, err := strconv.ParseFloat(lv.RAIDSyncPercent, 64); err == nil {
				syncPercent = int(parsed)
			}
		}

		lvHealth = append(lvHealth, lvmv1alpha1.RAIDLVHealth{
			Name:         lv.Name,
			RAIDType:     raidType,
			SyncPercent:  syncPercent,
			HealthStatus: lv.LVHealthStatus,
		})
	}

	if len(lvHealth) == 0 {
		return nil
	}

	unhealthyCount := 0
	for _, h := range lvHealth {
		if h.HealthStatus != "" {
			unhealthyCount++
		}
	}

	overallStatus := lvmv1alpha1.RAIDHealthStatusHealthy
	if unhealthyCount == len(lvHealth) {
		overallStatus = lvmv1alpha1.RAIDHealthStatusFailed
	} else if unhealthyCount > 0 {
		overallStatus = lvmv1alpha1.RAIDHealthStatusDegraded
	}

	if overallStatus == lvmv1alpha1.RAIDHealthStatusHealthy {
		for _, lv := range lvs {
			lvAttr, err := ParsedLvAttr(lv.LvAttr)
			if err != nil {
				continue
			}
			if lvAttr.VolumeType != VolumeTypeRAID && lvAttr.VolumeType != VolumeTypeRAIDNoInitialSync {
				continue
			}
			if lvAttr.Partial == PartialTrue {
				overallStatus = lvmv1alpha1.RAIDHealthStatusDegraded
				break
			}
		}
	}

	return &lvmv1alpha1.RAIDStatus{
		Status:   overallStatus,
		LVHealth: lvHealth,
	}
}
