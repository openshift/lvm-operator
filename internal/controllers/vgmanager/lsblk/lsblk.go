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
	HasBindMounts(b BlockDevice) (bool, string, error)
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

	err = json.Unmarshal([]byte(output), &blockDeviceMap)
	if err != nil {
		return []BlockDevice{}, err
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

	err = json.Unmarshal([]byte(output), &loopDeviceMap)
	if err != nil {
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

// HasBindMounts checks for bind mounts and returns mount point for a device by parsing `proc/1/mountinfo`.
// HostPID should be set to true inside the POD spec to get details of host's mount points inside `proc/1/mountinfo`.
func (lsblk *HostLSBLK) HasBindMounts(b BlockDevice) (bool, string, error) {
	file, err := os.Open(lsblk.mountInfo)
	if err != nil {
		return false, "", fmt.Errorf("failed to read file %s: %v", lsblk.mountInfo, err)
	}
	term := []byte(b.KName)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		mountInfo := scanner.Bytes()
		if bytes.Contains(mountInfo, term) {
			mountInfoList := bytes.Fields(mountInfo)
			if len(mountInfoList) >= 10 {
				// device source is 4th field for bind mounts and 10th for regular mounts
				if bytes.Equal(mountInfoList[3], []byte(fmt.Sprintf("/%s", filepath.Base(b.KName)))) || bytes.Equal(mountInfoList[9], term) {
					return true, string(mountInfoList[4]), nil
				}
			}
		}
	}

	return false, "", nil
}
