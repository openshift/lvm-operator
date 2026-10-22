package vgmanager

import (
	"context"
	"fmt"
	"testing"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDevicePathMappingsSkipsRegex(t *testing.T) {
	volumeGroup := &v1alpha1.LVMVolumeGroup{}
	volumeGroup.Spec.DeviceSelector = &v1alpha1.DeviceSelector{
		Paths:         []v1alpha1.DevicePath{"regex:/dev/sd[a-z]+", "/dev/disk/by-id/disk1"},
		OptionalPaths: []v1alpha1.DevicePath{"regex:/dev/nvme.*", "/dev/disk/by-id/missing", "/dev/disk/by-id/disk2"},
	}
	var resolved []string
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
		resolved = append(resolved, path)
		if path == "/dev/disk/by-id/missing" {
			return "", fmt.Errorf("missing")
		}
		return "/dev/" + path[len("/dev/disk/by-id/"):], nil
	})

	paths, err := buildDevicePathMappings(context.Background(), volumeGroup, resolver)
	require.NoError(t, err)
	assert.Equal(t, []string{"/dev/disk1", "/dev/disk2"}, paths)
	assert.Equal(t, []string{"/dev/disk/by-id/disk1", "/dev/disk/by-id/missing", "/dev/disk/by-id/disk2"}, resolved)
}
