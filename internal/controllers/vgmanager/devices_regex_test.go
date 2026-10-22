package vgmanager

import (
	"errors"
	"testing"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/stretchr/testify/require"
)

func TestVerifyMandatoryRegexDevicePaths(t *testing.T) {
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
		t.Errorf("resolver called for regex path %q", path)
		return "", nil
	})
	tests := []struct {
		name      string
		paths     []v1alpha1.DevicePath
		devices   FilteredBlockDevices
		errorPart string
	}{
		{name: "matches available device", paths: []v1alpha1.DevicePath{"regex:/dev/sd[a-z]+"}, devices: FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: "/dev/sda"}}}},
		{
			name:  "matches already setup device",
			paths: []v1alpha1.DevicePath{"regex:/dev/sd[a-z]+"},
			devices: FilteredBlockDevices{Excluded: []FilteredBlockDevice{{
				BlockDevice:  lsblk.BlockDevice{KName: "/dev/sda"},
				FilterErrors: []error{filter.ErrDeviceAlreadySetupCorrectly},
			}}},
		},
		{name: "does not match prefix", paths: []v1alpha1.DevicePath{"regex:/dev/sda"}, devices: FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: "/dev/sda1"}}}, errorPart: "does not match"},
		{name: "unusable match", paths: []v1alpha1.DevicePath{"regex:/dev/sda"}, devices: FilteredBlockDevices{Excluded: []FilteredBlockDevice{{BlockDevice: lsblk.BlockDevice{KName: "/dev/sda"}, FilterErrors: []error{errors.New("unusable")}}}}, errorPart: "does not match"},
		{name: "invalid regex", paths: []v1alpha1.DevicePath{"regex:/dev/["}, errorPart: "invalid mandatory device path regex"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyMandatoryDevicePaths(test.devices, resolver, test.paths)
			if test.errorPart == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.errorPart)
			}
		})
	}
}
