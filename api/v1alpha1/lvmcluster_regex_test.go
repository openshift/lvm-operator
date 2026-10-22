package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyRegexDevicePaths(t *testing.T) {
	tests := []struct {
		name      string
		path      DevicePath
		optional  bool
		errorPart string
	}{
		{name: "required regex", path: "regex:/dev/sd[a-z]+"},
		{name: "optional anchored regex", path: "regex:^/dev/nvme[0-9]+n[0-9]+$", optional: true},
		{name: "invalid required regex", path: "regex:/dev/[", errorPart: "invalid regex pattern"},
		{name: "invalid optional regex", path: "regex:/dev/(", optional: true, errorPart: "invalid regex pattern"},
		{name: "missing dev prefix", path: "regex:sd[a-z]+", errorPart: "must be an absolute path"},
		{name: "optional missing dev prefix", path: "regex:^/tmp/sd[a-z]+", optional: true, errorPart: "must be an absolute path"},
		{name: "empty regex", path: "regex:", errorPart: "must be an absolute path"},
		{name: "literal path", path: "/dev/sda"},
		{name: "invalid literal path", path: "/tmp/sda", errorPart: "must be an absolute path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector := &DeviceSelector{}
			if test.optional {
				selector.OptionalPaths = []DevicePath{test.path}
			} else {
				selector.Paths = []DevicePath{test.path}
			}
			cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{{DeviceSelector: selector}}}}}
			err := (&lvmClusterValidator{}).verifyAbsolutePath(cluster)
			if test.errorPart == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.errorPart)
				assert.ErrorContains(t, err, test.path.Unresolved())
			}
		})
	}
}

func TestVerifyNoDeviceOverlapSkipsRegex(t *testing.T) {
	cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
		{Name: "first", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"regex:/dev/sd[a-z]+"}}},
		{Name: "second", DeviceSelector: &DeviceSelector{OptionalPaths: []DevicePath{"regex:/dev/sd[a-z]+"}}},
	}}}}
	validator := &lvmClusterValidator{}
	require.NoError(t, validator.verifyNoDeviceOverlap(cluster))

	cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []DevicePath{"/dev/sda"}
	cluster.Spec.Storage.DeviceClasses[1].DeviceSelector.OptionalPaths = []DevicePath{"/dev/sda"}
	require.ErrorContains(t, validator.verifyNoDeviceOverlap(cluster), "overlaps in two different deviceClasss")
}
