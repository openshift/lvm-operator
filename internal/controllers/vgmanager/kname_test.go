package vgmanager

import (
	"path/filepath"
	"runtime"
)

func getKNameFromDevice(device string) string {
	// HACK: if we are on unix, we can simply use the "/tmp" path.
	// if we are on darwin, then the symlink of the temp file
	// will resolve to /private/var from /var, so we have to adjust
	// the block device name
	if runtime.GOOS == "darwin" {
		return filepath.Join("/", "private", device)
	}
	return device
}
