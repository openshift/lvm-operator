/*
Copyright 2021.

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

package vgmanager

import (
	"encoding/json"
	"fmt"

	"github.com/red-hat-storage/lvm-operator/pkg/internal"
)

const (
	lvmPath = "/usr/sbin/lvm"
)

// VolumeGroup represents a volume group of linux lvm.
type VolumeGroup struct {
	Name string `json:"vg_name"`
}

// ListVolumeGroups lists all volume groups.
func ListVolumeGroups(exec internal.Executor) ([]VolumeGroup, error) {
	// volumeGroupMap type is used for marshalling the `vgs --reportformt json` command output
	// Sample output:
	// `{"report": [ {"vg": [{"vg_name":"fedora", "pv_count":"1", "lv_count":"3", "snap_count":"0", "vg_attr":"wz--n-", "vg_size":"<475.94g", "vg_free":"0 "}]}]`
	volumeGroupMap := map[string][]map[string][]VolumeGroup{}
	arg := []string{
		"vgs", "--reportformat", "json",
	}
	output, err := exec.ExecuteCommandWithOutputAsHost(lvmPath, arg...)
	if err != nil {
		return nil, fmt.Errorf("failed to list volume groups. %v", err)
	}

	err = json.Unmarshal([]byte(output), &volumeGroupMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal volume group response. %v", err)
	}

	return volumeGroupMap["report"][0]["vg"], nil
}
