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
	FSType     string        `json:"fsType"`
	Size       string        `json:"size"`
	Children   []BlockDevice `json:"children,omitempty"`
	Rotational string        `json:"rota"`
	ReadOnly   string        `json:"ro,omitempty"`
	Removable  string        `json:"rm,omitempty"`
	PathByID   string        `json:"pathByID,omitempty"`
	Serial     string        `json:"serial,omitempty"`
	PartLabel  string        `json:"partLabel,omitempty"`
}

// ListBlockDevices using the lsblk command
func ListBlockDevices(exec Executor) ([]BlockDevice, error) {
	// var output bytes.Buffer
	var blockDeviceMap map[string][]BlockDevice
	columns := "NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,RM,STATE,KNAME,SERIAL,PARTLABEL"
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

// IsReadOnly checks is disk is read only
func (b BlockDevice) IsReadOnly() (bool, error) {
	return parseBitBool(b.ReadOnly)
}

// IsRemovable checks if a disk is removable
func (b BlockDevice) IsRemovable() (bool, error) {
	return parseBitBool(b.Removable)
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
