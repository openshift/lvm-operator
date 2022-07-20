package internal

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
)

var (
	mountFile = "/proc/1/mountinfo"
)

const (
	// StateSuspended is a possible value of BlockDevice.State
	StateSuspended = "suspended"

	// DeviceTypeLoop is the device type for loop devices in lsblk output
	DeviceTypeLoop = "loop"

	// DeviceTypeROM is the device type for ROM devices in lsblk output
	DeviceTypeROM = "rom"

	// mount string to find if a path is part of kubernetes
	pluginString = "plugins/kubernetes.io"

	DiskByNamePrefix = "/dev"
	DiskByPathPrefix = "/dev/disk/by-path"
)

// BlockDevice is the a block device as output by lsblk.
// All the fields are lsblk columns.
type BlockDevice struct {
	Name       string        `json:"name"`
	KName      string        `json:"kname"`
	Type       string        `json:"type"`
	Model      string        `json:"model,omitempty"`
	Vendor     string        `json:"vendor,omitempty"`
	State      string        `json:"state,omitempty"`
	FSType     string        `json:"fstype"`
	Size       string        `json:"size"`
	Children   []BlockDevice `json:"children,omitempty"`
	Rotational string        `json:"rota"`
	ReadOnly   string        `json:"ro,omitempty"`
	Serial     string        `json:"serial,omitempty"`
	PartLabel  string        `json:"partLabel,omitempty"`

	// DiskByPath is not part of lsblk output
	// fetch and set it only if specified in the CR
	DiskByPath string
}

// ListBlockDevices using the lsblk command
func ListBlockDevices(exec Executor) ([]BlockDevice, error) {
	// var output bytes.Buffer
	var blockDeviceMap map[string][]BlockDevice
	columns := "NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,STATE,KNAME,SERIAL,PARTLABEL,FSTYPE"
	args := []string{"--json", "-o", columns}

	output, err := exec.ExecuteCommandWithOutput("lsblk", args...)
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
func (b BlockDevice) IsUsableLoopDev(exec Executor) (bool, error) {

	// holds back-file string of the loop device
	var loopDeviceMap map[string][]struct {
		BackFile string `json:"back-file"`
	}

	usable := true

	args := []string{fmt.Sprintf("/dev/%s", b.Name), "-O", "BACK-FILE", "--json"}

	output, err := exec.ExecuteCommandWithOutput(losetupPath, args...)
	if err != nil {
		return usable, err
	}

	err = json.Unmarshal([]byte(output), &loopDeviceMap)
	if err != nil {
		return usable, err
	}

	for _, backFile := range loopDeviceMap["loopdevices"] {
		if strings.Contains(backFile.BackFile, pluginString) {
			// this loop device is being used by kubernetes and can't be
			// added to volume group
			usable = false
		}
	}

	return usable, nil
}

// IsReadOnly checks is disk is read only
func (b BlockDevice) IsReadOnly() (bool, error) {
	return parseBitBool(b.ReadOnly)
}

// HasChildren checks if the disk has partitions
func (b BlockDevice) HasChildren() bool {
	return len(b.Children) > 0
}

// HasBindMounts checks for bind mounts and returns mount point for a device by parsing `proc/1/mountinfo`.
// HostPID should be set to true inside the POD spec to get details of host's mount points inside `proc/1/mountinfo`.
func (b BlockDevice) HasBindMounts() (bool, string, error) {
	data, err := ioutil.ReadFile(mountFile)
	if err != nil {
		return false, "", fmt.Errorf("failed to read file %s: %v", mountFile, err)
	}

	mountString := string(data)
	for _, mountInfo := range strings.Split(mountString, "\n") {
		if strings.Contains(mountInfo, b.KName) {
			mountInfoList := strings.Split(mountInfo, " ")
			if len(mountInfoList) >= 10 {
				// device source is 4th field for bind mounts and 10th for regular mounts
				if mountInfoList[3] == fmt.Sprintf("/%s", b.KName) || mountInfoList[9] == fmt.Sprintf("/dev/%s", b.KName) {
					return true, mountInfoList[4], nil
				}
			}
		}
	}

	return false, "", nil
}

func parseBitBool(s string) (bool, error) {
	switch s {
	case "0", "false", "":
		return false, nil
	case "1", "true":
		return true, nil
	}
	return false, fmt.Errorf("invalid value: %q", s)
}
