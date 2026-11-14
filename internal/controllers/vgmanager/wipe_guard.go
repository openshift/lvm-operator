package vgmanager

import (
	"fmt"
	"slices"
	"strings"

	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
)

// wipeableDeviceTypes mirrors the device types the selection filter accepts
// (usableDeviceType rejects only rom and lvm). A type the filter would hand to
// vgcreate but the guard refuses is not "protected", it is simply unusable: the
// guard excludes it from selection too, so a pattern plus force wipe would reject
// every loop or multipath device while the same disks listed literally succeed.
var wipeableDeviceTypes = []string{"disk", "part", lsblk.DeviceTypeLoop, "mpath"}

// protectedSignatures are container signatures whose payload lsblk cannot see and
// whose header destruction is unrecoverable. A closed LUKS volume has no children,
// no mountpoint and reports as a partition, so only this check stands between it
// and wipefs.
var protectedSignatures = []string{filter.FSTypeLVM2Member, "linux_raid_member", "swap", "crypto_LUKS"}

// globWipeDevice checks the target and its ancestors in the host lsblk tree.
// Mount information must be present; an unknown state is not safe to wipe.
func globWipeDevice(target string, devices []lsblk.BlockDevice, protected map[string][]string) (lsblk.BlockDevice, error) {
	var result lsblk.BlockDevice
	found := false
	var checkErr error
	var walk func(lsblk.BlockDevice, error)
	walk = func(dev lsblk.BlockDevice, ancestorErr error) {
		err := ancestorErr
		if dev.Mountpoints == nil {
			err = fmt.Errorf("mount state of %s is unknown", dev.KName)
		} else {
			for _, mount := range *dev.Mountpoints {
				if mount != nil && *mount != "" {
					err = fmt.Errorf("%s is mounted or active as swap at %s", dev.KName, *mount)
					break
				}
			}
		}
		if dev.KName == target {
			if found && result.MajorMinor != dev.MajorMinor {
				checkErr = fmt.Errorf("%s has inconsistent device identities", target)
			}
			if !found {
				result = dev
			}
			found = true
			if err == nil && len(protected[target]) > 0 {
				err = fmt.Errorf("%s is protected by the physical-volume inventory", target)
			}
			if err == nil && !slices.Contains(wipeableDeviceTypes, dev.Type) {
				err = fmt.Errorf("%s has unsupported device type %q", target, dev.Type)
			}
			if err == nil && (dev.ReadOnly || dev.State == "suspended") {
				err = fmt.Errorf("%s is read-only or suspended", target)
			}
			if err == nil && dev.HasChildren() {
				err = fmt.Errorf("%s has partitions or dependent devices", target)
			}
			if err == nil && dev.MajorMinor == "" {
				err = fmt.Errorf("%s has no stable device number", target)
			}
			if err == nil && slices.Contains(protectedSignatures, dev.FSType) {
				err = fmt.Errorf("%s has a protected %s signature", target, dev.FSType)
			}
			for _, label := range []string{"bios", "boot", "reserved"} {
				if err == nil && strings.Contains(strings.ToLower(dev.PartLabel), label) {
					err = fmt.Errorf("%s has a protected partition label", target)
				}
			}
			if err != nil {
				checkErr = err
			}
		}
		for _, child := range dev.Children {
			walk(child, err)
		}
	}
	for _, dev := range devices {
		walk(dev, nil)
	}
	if !found {
		return result, fmt.Errorf("%s is not present in the block device inventory", target)
	}
	if checkErr != nil {
		return result, checkErr
	}
	return result, nil
}

// filterGlobWipeCandidates keeps pre-wipe exclusions in effect during later
// reconciliations, including after the one-shot wipe marker has been recorded.
func filterGlobWipeCandidates(filtered FilteredBlockDevices, devices []lsblk.BlockDevice, protected map[string][]string) FilteredBlockDevices {
	available := make([]lsblk.BlockDevice, 0, len(filtered.Available))
	for _, device := range filtered.Available {
		if _, err := globWipeDevice(device.KName, devices, protected); err != nil {
			filtered.Excluded = append(filtered.Excluded, FilteredBlockDevice{BlockDevice: device, FilterErrors: []error{err}})
			continue
		}
		available = append(available, device)
	}
	filtered.Available = available
	return filtered
}
