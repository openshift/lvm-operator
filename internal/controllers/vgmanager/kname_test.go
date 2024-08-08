package vgmanager

import (
	"path/filepath"
	"runtime"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

func getKNameFromDevice(device string) v1alpha1.DevicePath {
	// HACK: if we are on unix, we can simply use the "/tmp" path.
	// if we are on darwin, then the symlink of the temp file
	// will resolve to /private/var from /var, so we have to adjust
	// the block device name
	if runtime.GOOS == "darwin" {
		return v1alpha1.DevicePath(filepath.Join("/", "private", device))
	}
	return v1alpha1.DevicePath(device)
}
