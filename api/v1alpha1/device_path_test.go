package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDevicePath(t *testing.T) {
	for _, tc := range []struct {
		path  DevicePath
		valid bool
	}{
		{path: "/dev/sda", valid: true},
		{path: "/dev/disk/by-id/wwn-123", valid: true},
		{path: "/dev/sd*", valid: true}, // Wildcards without glob: remain literal.
		{path: "glob:/dev/sd*", valid: true},
		{path: "glob:/dev/sd?", valid: true},
		{path: "glob:/dev/sd[a-z]", valid: true},
		{path: "glob:/dev/disk/by-id/wwn-*", valid: true},
		{path: ""},
		{path: "sda"},
		{path: "regex:/dev/sd.*"},
		{path: "glob:"},
		{path: "glob:dev/sd*"},
		{path: "glob:/tmp/sd*"},
		{path: "glob:/dev/../tmp/*"},
		{path: "glob:/dev/sd["},
		{path: "glob:/dev/sd\\"},
	} {
		t.Run(string(tc.path), func(t *testing.T) {
			cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{{
				Name: "vg1", DeviceSelector: &DeviceSelector{},
			}}}}}
			for _, optional := range []bool{false, true} {
				selector := &DeviceSelector{Paths: []DevicePath{tc.path}}
				if optional {
					selector.OptionalPaths, selector.Paths = selector.Paths, nil
				}
				cluster.Spec.Storage.DeviceClasses[0].DeviceSelector = selector
				err := (&lvmClusterValidator{}).verifyAbsolutePath(cluster)
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		})
	}
}

func TestRAIDGlobDeviceCount(t *testing.T) {
	cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{{
		Name:           "vg1",
		DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}},
		RAIDConfig:     &RAIDConfig{Type: RAIDTypeRAID10},
	}}}}}
	validator := &lvmClusterValidator{}
	// RAID geometry cannot be validated at admission for node-specific patterns.
	require.ErrorIs(t, validator.verifyRAIDConfig(cluster), ErrRAIDGlobPathsUnsupported)
	cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []DevicePath{"/dev/sda"}
	require.ErrorContains(t, validator.verifyRAIDConfig(cluster), "requires at least")
}
