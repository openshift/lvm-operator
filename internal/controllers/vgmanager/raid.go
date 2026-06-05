package vgmanager

import (
	"fmt"
	"strconv"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
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
