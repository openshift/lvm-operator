package lsblk

import (
	"encoding/json"
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
	defer func() {
		_ = output.Close()
	}()
	if err != nil {
		return []BlockDevice{}, err
	}

	if err = json.NewDecoder(output).Decode(&blockDeviceMap); err != nil {
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
	defer func() {
		_ = output.Close()
	}()
	if err != nil {
		return true, err
	}

	if err = json.NewDecoder(output).Decode(&loopDeviceMap); err != nil {
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

	blockDeviceInfos := make(BlockDeviceInfos)
	// file, err := os.Open(lsblk.mountInfo)
	// defer file.Close() // nolint:golint,staticcheck
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to read file %s: %v", lsblk.mountInfo, err)
	// }
	// data, err := io.ReadAll(file)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to read mount file %s: %v", lsblk.mountInfo, err)
	// }
	// scanner := bufio.NewScanner(bytes.NewReader(data))
	//
	// for scanner.Scan() {
	// 	mountInfo := scanner.Text()
	// 	mountInfoList := strings.Fields(mountInfo)
	// 	if len(mountInfoList) >= 10 {
	// 		for _, bd := range flattenedMap {
	// 			if blockDeviceInfos[bd.KName].HasBindMounts {
	// 				continue
	// 			}
	// 			if strings.Contains(mountInfo, bd.KName) {
	// 				// dev source is 4th field for bind mounts and 10th for regular mounts
	// 				if mountInfoList[3] == fmt.Sprintf("/%s", filepath.Base(bd.KName)) || mountInfoList[9] == bd.KName {
	// 					blockDeviceInfos[bd.KName] = BlockDeviceInfo{
	// 						HasBindMounts: true,
	// 					}
	// 					break
	// 				}
	// 			}
	// 		}
	// 	}
	// }
	// if scanner.Err() != nil {
	// 	return nil, fmt.Errorf("failed to mountinfo %s: %v", lsblk.mountInfo, scanner.Err())
	// }

	for _, dev := range flattenedMap {
		if dev.Type == "loop" {
			info := blockDeviceInfos[dev.KName]
			info.IsUsableLoopDev, _ = lsblk.IsUsableLoopDev(dev)
			blockDeviceInfos[dev.KName] = info
		}
	}

	return blockDeviceInfos, nil
}
