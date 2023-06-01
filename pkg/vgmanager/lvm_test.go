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

package vgmanager

import (
	"fmt"
	"strings"
	"testing"

	mockExec "github.com/openshift/lvm-operator/pkg/internal/test"
	"github.com/stretchr/testify/assert"
)

var mockVgsOutput = `{
	"report": [
		{
			"vg": [
				{"vg_name":"vg1", "pv_count":"3", "lv_count":"3", "snap_count":"0", "vg_attr":"wz--n-", "vg_size":"<475.94g", "vg_free":"0 "},
				{"vg_name":"vg2", "pv_count":"3", "lv_count":"3", "snap_count":"0", "vg_attr":"wz--n-", "vg_size":"<475.94g", "vg_free":"0 "}
			]
		}
	]
}`

var mockPvsOutputForVG1 = `
{
	"report": [
		{
			"pv": [
				{"pv_name":"/dev/sda", "vg_name":"vg1", "pv_fmt":"lvm2", "pv_attr":"a--", "pv_size":"<475.94g", "pv_free":"0 "},
				{"pv_name":"/dev/sdb", "vg_name":"vg1", "pv_fmt":"lvm2", "pv_attr":"a--", "pv_size":"<475.94g", "pv_free":"0 "},
				{"pv_name":"/dev/sdc", "vg_name":"vg1", "pv_fmt":"lvm2", "pv_attr":"a--", "pv_size":"<475.94g", "pv_free":"0 "}
			]
		}
	]
}
`

var mockPvsOutputForVG2 = `
{
	"report": [
		{
			"pv": [
				{"pv_name":"/dev/sdd", "vg_name":"vg2", "pv_fmt":"lvm2", "pv_attr":"a--", "pv_size":"<475.94g", "pv_free":"0 "},
				{"pv_name":"/dev/sde", "vg_name":"vg2", "pv_fmt":"lvm2", "pv_attr":"a--", "pv_size":"<475.94g", "pv_free":"0 "}
			]
		}
	]
}
`

func TestGetVolumeGroup(t *testing.T) {
	tests := []struct {
		name    string
		vgName  string
		pvCount int
		wantErr bool
	}{
		{"Invalid volume group name", "invalid-vg", 0, true},
		{"Valid volume group name", "vg1", 3, false},
		{"Valid volume group name", "vg2", 2, false},
	}
	executor := &mockExec.MockExecutor{
		MockExecuteCommandWithOutputAsHost: func(command string, args ...string) (string, error) {
			if args[0] == "vgs" {
				return mockVgsOutput, nil
			} else if args[0] == "pvs" {
				if strings.HasSuffix(args[2], "vg1") {
					return mockPvsOutputForVG1, nil
				} else if strings.HasSuffix(args[2], "vg2") {
					return mockPvsOutputForVG2, nil
				}
			}
			return "", fmt.Errorf("invalid args %q", args[0])
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vg, err := GetVolumeGroup(executor, tt.vgName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.vgName, vg.Name)
				assert.Equal(t, tt.pvCount, len(vg.PVs))
			}
		})
	}
}

func TestListVolumeGroup(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"List all volume groups", false},
	}
	executor := &mockExec.MockExecutor{
		MockExecuteCommandWithOutputAsHost: func(command string, args ...string) (string, error) {
			if args[0] == "vgs" {
				return mockVgsOutput, nil
			} else if args[0] == "pvs" {
				if strings.HasSuffix(args[2], "vg1") {
					return mockPvsOutputForVG1, nil
				} else if strings.HasSuffix(args[2], "vg2") {
					return mockPvsOutputForVG2, nil
				}
			}
			return "", fmt.Errorf("invalid args %q", args[0])
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vgs, err := ListVolumeGroups(executor)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				for _, vg := range vgs {
					if vg.Name == "vg1" {
						assert.Equal(t, 3, len(vg.PVs))
					} else if vg.Name == "vg2" {
						assert.Equal(t, 2, len(vg.PVs))
					}
				}
			}
		})
	}
}

func TestCreateVolumeGroup(t *testing.T) {
	tests := []struct {
		name        string
		volumeGroup *VolumeGroup
		pvs         []string
		wantErr     bool
	}{
		{"No Volume Group Name", &VolumeGroup{}, []string{}, true},
		{"No Physical Volumes", &VolumeGroup{Name: "vg1"}, []string{}, true},
		{"Volume Group created successfully", &VolumeGroup{Name: "vg1"}, []string{"/dev/sdb"}, false},
	}

	executor := &mockExec.MockExecutor{
		MockExecuteCommandWithOutputAsHost: func(command string, args ...string) (string, error) {
			return "", nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.volumeGroup.Create(executor, tt.pvs)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExtendVolumeGroup(t *testing.T) {
	tests := []struct {
		name        string
		volumeGroup *VolumeGroup
		PVs         []string
		wantErr     bool
	}{
		{"No PVs are available", &VolumeGroup{Name: "vg1"}, []string{}, true},
		{"New PVs are available", &VolumeGroup{Name: "vg1"}, []string{"/dev/sdb", "/dev/sdc"}, false},
	}

	executor := &mockExec.MockExecutor{
		MockExecuteCommandWithOutputAsHost: func(command string, args ...string) (string, error) {
			return "", nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.volumeGroup.Extend(executor, tt.PVs)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
