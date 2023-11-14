package lsblk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/exec"
)

var (
	DefaultMountinfo = "/proc/1/mountinfo"
	DefaultLosetup   = "/usr/sbin/losetup"
	DefaultLsblk     = "/usr/bin/lsblk"
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
	Name      string        `json:"name"`
	KName     string        `json:"kname"`
	Type      string        `json:"type"`
	Model     string        `json:"model,omitempty"`
	Vendor    string        `json:"vendor,omitempty"`
	State     string        `json:"state,omitempty"`
	FSType    string        `json:"fstype"`
	Size      string        `json:"size"`
	Children  []BlockDevice `json:"children,omitempty"`
	ReadOnly  bool          `json:"ro,omitempty"`
	Serial    string        `json:"serial,omitempty"`
	PartLabel string        `json:"partLabel,omitempty"`

	// DevicePath is the path given by user
	DevicePath string
}

type LSBLK interface {
	ListBlockDevices() ([]BlockDevice, error)
	IsUsableLoopDev(b BlockDevice) (bool, error)
	BlockDeviceInfos(bs []BlockDevice) (BlockDeviceInfos, error)
}

type HostLSBLK struct {
	exec.Executor
	lsblk     string
	mountInfo string
	losetup   string
}

func NewDefaultHostLSBLK() *HostLSBLK {
	return NewHostLSBLK(&exec.CommandExecutor{}, DefaultLsblk, DefaultMountinfo, DefaultLosetup)
}

func NewHostLSBLK(executor exec.Executor, lsblk, mountInfo, losetup string) *HostLSBLK {
	hostLsblk := &HostLSBLK{
		lsblk:     lsblk,
		Executor:  executor,
		mountInfo: mountInfo,
		losetup:   losetup,
	}
	return hostLsblk
}

// HasChildren checks if the disk has partitions
func (b BlockDevice) HasChildren() bool {
	return len(b.Children) > 0
}

// ListBlockDevices lists the block devices using the lsblk command
func (lsblk *HostLSBLK) ListBlockDevices() ([]BlockDevice, error) {
	// var output bytes.Buffer
	var blockDeviceMap map[string][]BlockDevice
	columns := "NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,STATE,KNAME,SERIAL,PARTLABEL,FSTYPE"
	args := []string{"--json", "--paths", "-o", columns}

	output, err := lsblk.ExecuteCommandWithOutput(lsblk.lsblk, args...)
	if err != nil {
		return []BlockDevice{}, err
	}

	if err = json.NewDecoder(strings.NewReader(output)).Decode(&blockDeviceMap); err != nil {
		return nil, err
	}

	return blockDeviceMap["blockdevices"], nil
}

// IsUsableLoopDev returns true if the loop device isn't in use by Kubernetes
// by matching the back file path against a standard string used to mount devices
// from host into pods
func (lsblk *HostLSBLK) IsUsableLoopDev(b BlockDevice) (bool, error) {
	// holds back-file string of the loop device
	var loopDeviceMap map[string][]struct {
		BackFile string `json:"back-file"`
	}

	args := []string{b.Name, "-O", "BACK-FILE", "--json"}
	output, err := lsblk.ExecuteCommandWithOutput(lsblk.losetup, args...)
	if err != nil {
		return true, err
	}

	if err = json.NewDecoder(strings.NewReader(output)).Decode(&loopDeviceMap); err != nil {
		return false, err
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
	HasBindMounts   bool
	IsUsableLoopDev bool
}

func flattenedBlockDevices(bs []BlockDevice) map[string]BlockDevice {
	flattened := make(map[string]BlockDevice, len(bs))
	for _, b := range bs {
		flattened[b.KName] = b
		if b.HasChildren() {
			for k, v := range flattenedBlockDevices(b.Children) {
				flattened[k] = v
			}
		}
	}
	return flattened
}

func (lsblk *HostLSBLK) BlockDeviceInfos(bs []BlockDevice) (BlockDeviceInfos, error) {
	flattenedMap := flattenedBlockDevices(bs)
	flattened := make([]BlockDevice, len(flattenedMap))
	i := 0
	for _, v := range flattenedMap {
		flattened[i] = v
		i++
	}

	file, err := os.Open(lsblk.mountInfo)
	defer file.Close() // nolint:golint,staticcheck
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %v", lsblk.mountInfo, err)
	}
	scanner := bufio.NewScanner(file)

	blockDeviceInfos := make(BlockDeviceInfos)
	for scanner.Scan() {
		mountInfo := scanner.Bytes()
		mountInfoList := bytes.Fields(mountInfo)
		if len(mountInfoList) >= 10 {
			for _, bd := range flattened {
				if bytes.Contains(mountInfo, []byte(bd.KName)) {
					// dev source is 4th field for bind mounts and 10th for regular mounts
					if bytes.Equal(mountInfoList[3], []byte(fmt.Sprintf("/%s", filepath.Base(bd.KName)))) || bytes.Equal(mountInfoList[9], []byte(bd.KName)) {
						blockDeviceInfos[bd.KName] = BlockDeviceInfo{
							HasBindMounts: true,
						}
						break
					}
				}
			}
		}
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("failed to mountinfo %s: %v", lsblk.mountInfo, scanner.Err())
	}

	bindMountMap := make(BlockDeviceInfos)
	for _, dev := range flattened {
		if dev.Type == "loop" {
			info := bindMountMap[dev.KName]
			info.IsUsableLoopDev, _ = lsblk.IsUsableLoopDev(dev)
			bindMountMap[dev.KName] = info
		}
	}

	return bindMountMap, nil
}
