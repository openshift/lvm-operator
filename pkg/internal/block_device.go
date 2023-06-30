/*
Copyright © 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	// DeviceTypeLVM is the device type for lvm devices in lsblk output
	DeviceTypeLVM = "lvm"

	// mount string to find if a path is part of kubernetes
	pluginString = "plugins/kubernetes.io"
)

// BlockDevice is the block device as output by lsblk.
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
	Rotational bool          `json:"rota"`
	ReadOnly   bool          `json:"ro,omitempty"`
	Serial     string        `json:"serial,omitempty"`
	PartLabel  string        `json:"partLabel,omitempty"`

	// DevicePath is the path given by user
	DevicePath string
}

// ListBlockDevices lists the block devices using the lsblk command
func ListBlockDevices(exec Executor) ([]BlockDevice, error) {
	// var output bytes.Buffer
	var blockDeviceMap map[string][]BlockDevice
	columns := "NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,STATE,KNAME,SERIAL,PARTLABEL,FSTYPE"
	args := []string{"--json", "--paths", "-o", columns}

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
	args := []string{b.Name, "-O", "BACK-FILE", "--json"}
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

// HasChildren checks if the disk has partitions
func (b BlockDevice) HasChildren() bool {
	return len(b.Children) > 0
}

// HasBindMounts checks for bind mounts and returns mount point for a device by parsing `proc/1/mountinfo`.
// HostPID should be set to true inside the POD spec to get details of host's mount points inside `proc/1/mountinfo`.
func (b BlockDevice) HasBindMounts() (bool, string, error) {
	data, err := os.ReadFile(mountFile)
	if err != nil {
		return false, "", fmt.Errorf("failed to read file %s: %v", mountFile, err)
	}

	mountString := string(data)
	for _, mountInfo := range strings.Split(mountString, "\n") {
		if strings.Contains(mountInfo, b.KName) {
			mountInfoList := strings.Split(mountInfo, " ")
			if len(mountInfoList) >= 10 {
				// device source is 4th field for bind mounts and 10th for regular mounts
				if mountInfoList[3] == fmt.Sprintf("/%s", filepath.Base(b.KName)) || mountInfoList[9] == b.KName {
					return true, mountInfoList[4], nil
				}
			}
		}
	}

	return false, "", nil
}
