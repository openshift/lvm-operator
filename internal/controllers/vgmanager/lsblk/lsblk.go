package lsblk

import (
	"context"
	"strings"

	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec"
)

var (
	DefaultLosetup = "/usr/sbin/losetup"
	DefaultLsblk   = "/usr/bin/lsblk"
)

const (
	// mount string to find if a path is part of kubernetes
	pluginString = "plugins/kubernetes.io"
)

const (
	// DeviceTypeLoop is the device type for loop devices in lsblk output
	DeviceTypeLoop = "loop"

	// DeviceTypeROM is the device type for ROM devices in lsblk output
	DeviceTypeROM = "rom"

	// DeviceTypeLVM is the device type for lvm devices in lsblk output
	DeviceTypeLVM = "lvm"
)

// BlockDevice is the block device as output by lsblk.
// All the fields are lsblk columns.
type BlockDevice struct {
	Name        string        `json:"name"`
	KName       string        `json:"kname"`
	Type        string        `json:"type"`
	Model       string        `json:"model,omitempty"`
	Vendor      string        `json:"vendor,omitempty"`
	State       string        `json:"state,omitempty"`
	FSType      string        `json:"fstype"`
	Size        string        `json:"size"`
	Children    []BlockDevice `json:"children,omitempty"`
	ReadOnly    bool          `json:"ro,omitempty"`
	Serial      string        `json:"serial,omitempty"`
	WWN         string        `json:"wwn,omitempty"`
	PartLabel   string        `json:"partLabel,omitempty"`
	MajorMinor  string        `json:"maj:min,omitempty"`
	Mountpoints *[]*string    `json:"mountpoints,omitempty"`
}

type LSBLK interface {
	ListBlockDevices(ctx context.Context) ([]BlockDevice, error)
	IsUsableLoopDev(ctx context.Context, b BlockDevice) (bool, error)
	BlockDeviceInfos(ctx context.Context, bs []BlockDevice) (BlockDeviceInfos, error)
}

type HostLSBLK struct {
	exec.Executor
	lsblk   string
	losetup string
}

func NewDefaultHostLSBLK() *HostLSBLK {
	return NewHostLSBLK(&exec.CommandExecutor{}, DefaultLsblk, DefaultLosetup)
}

func NewHostLSBLK(executor exec.Executor, lsblk, losetup string) *HostLSBLK {
	hostLsblk := &HostLSBLK{
		lsblk:    lsblk,
		Executor: executor,
		losetup:  losetup,
	}
	return hostLsblk
}

// HasChildren checks if the disk has partitions
func (b BlockDevice) HasChildren() bool {
	return len(b.Children) > 0
}

// LSBLK_COLUMNS requires util-linux 2.37 or newer on the node, because MOUNTPOINTS
// was added in that release. lsblk runs on the host, so this is the node's version,
// not the container's. Measured: util-linux 2.37.4 (RHEL 9) accepts the set, while
// 2.32.1 (RHEL 8) rejects it with "unknown column: MOUNTPOINTS".
//
// An unknown column makes lsblk exit non-zero, so ListBlockDevices fails and the
// node discovers no devices at all. Every OpenShift release this operator ships
// with runs RHEL 9 or later nodes, so the floor is met; the hazard is backporting
// the wildcard selector feature, which introduced MOUNTPOINTS, to a branch that
// still supports RHEL 8 nodes. That would need a singular-MOUNTPOINT fallback, and
// the fallback must keep reporting every mount: globWipeDevice refuses to wipe a
// device whose mount state is unknown, and would be weakened by seeing only one.
//
// Before adding a column here, check it against the oldest supported node OS.
const LSBLK_COLUMNS = "NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,STATE,KNAME,SERIAL,WWN,PARTLABEL,FSTYPE,MAJ:MIN,MOUNTPOINTS"

// ListBlockDevices lists the block devices using the lsblk command
func (lsblk *HostLSBLK) ListBlockDevices(ctx context.Context) ([]BlockDevice, error) {
	// var output bytes.Buffer
	var blockDeviceMap map[string][]BlockDevice
	args := []string{"--json", "--paths", "-o", LSBLK_COLUMNS}

	if err := lsblk.RunCommandAsHostInto(ctx, &blockDeviceMap, lsblk.lsblk, args...); err != nil {
		return []BlockDevice{}, err
	}

	return blockDeviceMap["blockdevices"], nil
}

// IsUsableLoopDev returns true if the loop device isn't in use by Kubernetes
// by matching the back file path against a standard string used to mount devices
// from host into pods
func (lsblk *HostLSBLK) IsUsableLoopDev(ctx context.Context, b BlockDevice) (bool, error) {
	// holds back-file string of the loop device
	var loopDeviceMap map[string][]struct {
		BackFile string `json:"back-file"`
	}

	args := []string{b.Name, "-O", "BACK-FILE", "--json"}
	if err := lsblk.RunCommandAsHostInto(ctx, &loopDeviceMap, lsblk.losetup, args...); err != nil {
		return true, err
	}

	for _, backFile := range loopDeviceMap["loopdevices"] {
		if strings.Contains(backFile.BackFile, pluginString) {
			// this loop device is being used by kubernetes and can't be
			// added to volume group
			return false, nil
		}
	}

	return true, nil
}

type BlockDeviceInfos map[string]BlockDeviceInfo

type BlockDeviceInfo struct {
	IsUsableLoopDev bool
}

func FlattenedBlockDevices(bs []BlockDevice) map[string]BlockDevice {
	flattened := make(map[string]BlockDevice, len(bs))
	for _, b := range bs {
		flattened[b.KName] = b
		if b.HasChildren() {
			for k, v := range FlattenedBlockDevices(b.Children) {
				flattened[k] = v
			}
		}
	}
	return flattened
}

func (lsblk *HostLSBLK) BlockDeviceInfos(ctx context.Context, bs []BlockDevice) (BlockDeviceInfos, error) {
	flattenedMap := FlattenedBlockDevices(bs)

	blockDeviceInfos := make(BlockDeviceInfos)

	for _, dev := range flattenedMap {
		if dev.Type == "loop" {
			info := blockDeviceInfos[dev.KName]
			info.IsUsableLoopDev, _ = lsblk.IsUsableLoopDev(ctx, dev)
			blockDeviceInfos[dev.KName] = info
		}
	}

	return blockDeviceInfos, nil
}
