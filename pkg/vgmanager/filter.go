package vgmanager

import (
	"strings"

	"github.com/red-hat-storage/lvm-operator/pkg/internal"
)

const (
	// filter names:
	notReadOnly           = "notReadOnly"
	notRemovable          = "notRemovable"
	notSuspended          = "notSuspended"
	noBiosBootInPartLabel = "noBiosBootInPartLabel"
	noFilesystemSignature = "noFilesystemSignature"
	noBindMounts          = "noBindMounts"
	noChildren            = "noChildren"
	canOpenExclusively    = "canOpenExclusively"
)

// filterAvailableDevices returns:
// validDevices: the list of blockdevices considered available
// delayedDevices: the list of blockdevices considered available, but first observed less than 'minDeviceAge' time ago
// error
func (r *VGReconciler) filterAvailableDevices(blockDevices []internal.BlockDevice) ([]internal.BlockDevice, []internal.BlockDevice, error) {
	var availableDevices, delayedDevices []internal.BlockDevice
	// using a label so `continue DeviceLoop` can be used to skip devices
DeviceLoop:
	for _, blockDevice := range blockDevices {

		// store device in deviceAgeMap
		r.deviceAgeMap.storeDeviceAge(blockDevice.KName)

		devLogger := r.Log.WithValues("Device.Name", blockDevice.Name)
		for name, filter := range FilterMap {
			var valid bool
			var err error
			filterLogger := devLogger.WithValues("filter.Name", name)
			valid, err = filter(blockDevice)
			if err != nil {
				filterLogger.Error(err, "filter error")
				valid = false
				continue DeviceLoop
			} else if !valid {
				filterLogger.Info("filter not passed")
				continue DeviceLoop
			}
		}
		// check if the device is older than deviceMinAge
		isOldEnough := r.deviceAgeMap.isOlderThan(blockDevice.KName)
		if isOldEnough {
			availableDevices = append(availableDevices, blockDevice)
		} else {
			delayedDevices = append(delayedDevices, blockDevice)
		}
	}
	return availableDevices, delayedDevices, nil
}

// maps of function identifier (for logs) to filter function.
// These are passed the localv1alpha1.DeviceInclusionSpec to make testing easier,
// but they aren't expected to use it
// they verify that the device itself is good to use
var FilterMap = map[string]func(internal.BlockDevice) (bool, error){
	notReadOnly: func(dev internal.BlockDevice) (bool, error) {
		readOnly, err := dev.GetReadOnly()
		return !readOnly, err
	},

	notRemovable: func(dev internal.BlockDevice) (bool, error) {
		removable, err := dev.GetRemovable()
		return !removable, err
	},

	notSuspended: func(dev internal.BlockDevice) (bool, error) {
		matched := dev.State != internal.StateSuspended
		return matched, nil
	},

	noBiosBootInPartLabel: func(dev internal.BlockDevice) (bool, error) {
		biosBootInPartLabel := strings.Contains(strings.ToLower(dev.PartLabel), strings.ToLower("bios")) ||
			strings.Contains(strings.ToLower(dev.PartLabel), strings.ToLower("boot"))
		return !biosBootInPartLabel, nil
	},

	noFilesystemSignature: func(dev internal.BlockDevice) (bool, error) {
		matched := dev.FSType == ""
		return matched, nil
	},
	noBindMounts: func(dev internal.BlockDevice) (bool, error) {
		hasBindMounts, _, err := dev.HasBindMounts()
		return !hasBindMounts, err
	},

	noChildren: func(dev internal.BlockDevice) (bool, error) {
		hasChildren, err := dev.HasChildren()
		return !hasChildren, err
	},
	canOpenExclusively: func(dev internal.BlockDevice) (bool, error) {
		// todo(rohan): fix permission error
		return true, nil
		// pathname, err := dev.GetDevPath()
		// if err != nil {
		// 	return false, fmt.Errorf("pathname: %q: %w", pathname, err)
		// }
		// fd, errno := unix.Open(pathname, unix.O_RDONLY|unix.O_EXCL, 0)
		// // If the device is in use, open will return an invalid fd.
		// // When this happens, it is expected that Close will fail and throw an error.
		// defer unix.Close(fd)
		// if errno == nil {
		// 	// device not in use
		// 	return true, nil
		// } else if errno == unix.EBUSY {
		// 	// device is in use
		// 	return false, nil
		// }
		// // error during call to Open
		// return false, fmt.Errorf("pathname: %q: %w", pathname, errno)

	},
}
