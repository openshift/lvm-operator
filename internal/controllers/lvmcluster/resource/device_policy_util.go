package resource

import (
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

func RequiresSharedVolumeGroupSetup(dcs []lvmv1alpha1.DeviceClass) (
	shared bool,
	standard bool,
) {
	for _, dc := range dcs {
		if dc.DeviceAccessPolicy == lvmv1alpha1.DeviceAccessPolicyShared {
			shared = true
		}
		if dc.DeviceAccessPolicy == lvmv1alpha1.DeviceAccessPolicyNodeLocal {
			standard = true
		}
	}
	return
}
