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

package lvm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr/testr"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec/test"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

func TestHostLVM_GetVG(t *testing.T) {
	tests := []struct {
		name    string
		vgName  string
		pvCount int
		wantErr bool
		vgsErr  bool
		pvsErr  bool
	}{
		{"Invalid volume group name", "invalid-vg", 0, true, false, false},
		{"Valid volume group name", "vg1", 3, false, false, false},
		{"Valid volume group name", "vg2", 2, false, false, false},
		{"Valid volume group name but vgs fails", "vg2", 2, true, true, false},
		{"Valid volume group name but pvs fails", "vg2", 2, true, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{
				MockRunCommandAsHostInto: func(ctx context.Context, into any, command string, args ...string) error {
					if command == vgsCmd {
						if tt.vgsErr {
							return fmt.Errorf("mocked error")
						}

						return json.Unmarshal([]byte(mockVgsOutput), &into)
					} else if command == pvsCmd {
						if tt.pvsErr {
							return fmt.Errorf("mocked error")
						}
						argsConcat := strings.Join(args, " ")
						out := "--units g -v --reportformat json -S vgname=%s"
						if argsConcat == fmt.Sprintf(out, "vg1") {
							return json.Unmarshal([]byte(mockPvsOutputForVG1), &into)
						} else if argsConcat == fmt.Sprintf(out, "vg2") {
							return json.Unmarshal([]byte(mockPvsOutputForVG2), &into)
						}
					}
					return fmt.Errorf("invalid args %q", args[0])
				},
			}
			vg, err := NewHostLVM(executor).GetVG(ctx, tt.vgName)
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

type MockedExitError struct {
	exitCode int
}

func (m *MockedExitError) Error() string {
	return fmt.Sprintf("exit status %d", m.exitCode)
}

func (m *MockedExitError) ExitCode() int {
	return m.exitCode
}

func (m *MockedExitError) Unwrap() error {
	return m
}

func TestHostLVM_DeleteVG(t *testing.T) {
	tests := []struct {
		name          string
		volumeGroup   VolumeGroup
		wantErr       bool
		vgChangeErr   bool
		vgRemoveErr   bool
		pvRemoveErr   bool
		lvmdevicesErr ExitError
	}{
		{
			name:        "Delete VG",
			volumeGroup: VolumeGroup{Name: "vg1"},
		},
		{
			name:        "Delete VG and VGChange deactivation fails",
			volumeGroup: VolumeGroup{Name: "vg1"},
			wantErr:     true,
			vgChangeErr: true,
		},
		{
			name:        "Delete VG and VGRemove fails",
			volumeGroup: VolumeGroup{Name: "vg1"},
			wantErr:     true,
			vgRemoveErr: true,
		},
		{
			name:        "Delete VG and underlying PVRemove fails",
			volumeGroup: VolumeGroup{Name: "vg1", PVs: []PhysicalVolume{{PvName: "/dev/sdb"}}},
			wantErr:     true,
			pvRemoveErr: true,
		},
		{
			name:          "Delete VG and underlying PVID removal fails",
			volumeGroup:   VolumeGroup{Name: "vg1", PVs: []PhysicalVolume{{PvName: "/dev/sdb"}}},
			wantErr:       true,
			lvmdevicesErr: &MockedExitError{exitCode: 1},
		},
		{
			name:          "Delete VG and underlying PVID removal fails (device is not found)",
			volumeGroup:   VolumeGroup{Name: "vg1", PVs: []PhysicalVolume{{PvName: "/dev/sdb"}}},
			lvmdevicesErr: &MockedExitError{exitCode: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{
				MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
					switch command {
					case vgChangeCmd:
						if tt.vgChangeErr {
							return fmt.Errorf("mocked error")
						}
						assert.ElementsMatch(t, args, []string{"-an", tt.volumeGroup.Name})
					case vgRemoveCmd:
						if tt.vgRemoveErr {
							return fmt.Errorf("mocked error")
						}
						assert.ElementsMatch(t, args, []string{tt.volumeGroup.Name})
					case pvRemoveCmd:
						if tt.pvRemoveErr {
							return fmt.Errorf("mocked error")
						}
						var pvArgs []string
						for _, pv := range tt.volumeGroup.PVs {
							pvArgs = append(pvArgs, pv.PvName)
						}
						assert.ElementsMatch(t, args, pvArgs)
					case lvmDevicesCmd:
						if tt.lvmdevicesErr != nil {
							return tt.lvmdevicesErr
						}
						assert.ElementsMatch(t, args, []string{"--delpvid"})
						for _, pv := range tt.volumeGroup.PVs {
							if pv.UUID == args[2] {
								assert.ElementsMatch(t, args, []string{"--delpvid", pv.UUID})
							}
						}
					}

					return nil
				},
			}

			err := NewHostLVM(executor).DeleteVG(ctx, tt.volumeGroup)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostLVM_ListVGs(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
		vgsErr  bool
		pvsErr  bool
	}{
		{"List all volume groups", false, false, false},
		{"List all volume groups but vgs fails", true, true, false},
		{"List all volume groups but pvs fails", true, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{
				MockRunCommandAsHostInto: func(ctx context.Context, into any, command string, args ...string) error {
					if command == vgsCmd {
						if tt.vgsErr {
							return fmt.Errorf("mocked error on vgs")
						}
						return json.Unmarshal([]byte(mockVgsOutput), &into)
					} else if command == pvsCmd {
						if tt.pvsErr {
							return fmt.Errorf("mocked error on pvs")
						}
						return json.Unmarshal([]byte(mockPvsOutputForVG1), &into)
					}
					return fmt.Errorf("invalid args %q", args[0])
				},
			}
			_, err := NewHostLVM(executor).ListVGs(ctx, true)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostLVM_CreateVG(t *testing.T) {
	tests := []struct {
		name        string
		volumeGroup VolumeGroup
		wantErr     bool
		execErr     bool
	}{
		{"No Volume Group Name", VolumeGroup{}, true, false},
		{"No Physical Volumes", VolumeGroup{Name: "vg1"}, true, false},
		{"Volume Group created successfully", VolumeGroup{Name: "vg1", PVs: []PhysicalVolume{{PvName: "/dev/sdb"}}}, false, false},
		{"Volume Group created failed because of vgcreate", VolumeGroup{Name: "vg1", PVs: []PhysicalVolume{{PvName: "/dev/sdb"}}}, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{
				MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
					if tt.execErr {
						return fmt.Errorf("mocked error")
					}
					return nil
				},
			}
			err := NewHostLVM(executor).CreateVG(ctx, tt.volumeGroup)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostLVM_ExtendVG(t *testing.T) {
	tests := []struct {
		name        string
		volumeGroup VolumeGroup
		PVs         []string
		wantErr     bool
		execErr     bool
	}{
		{"Empty Volume Group Name", VolumeGroup{}, []string{}, true, false},
		{"Error on Exec", VolumeGroup{Name: "vg1"}, []string{}, true, true},
		{"No PVs are available", VolumeGroup{Name: "vg1"}, []string{}, true, false},
		{"New PVs are available", VolumeGroup{Name: "vg1"}, []string{"/dev/sdb", "/dev/sdc"}, false, false},
		{"New PVs are available but extend fails", VolumeGroup{Name: "vg1"}, []string{"/dev/sdb", "/dev/sdc"}, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
				if tt.execErr {
					return fmt.Errorf("mocked error")
				}
				return nil
			}}

			newVG, err := NewHostLVM(executor).ExtendVG(ctx, tt.volumeGroup, tt.PVs)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				newPVs := make([]string, len(newVG.PVs))
				for i, pv := range newVG.PVs {
					newPVs[i] = pv.PvName
				}
				assert.ElementsMatch(t, newPVs, tt.PVs)
			}
		})
	}
}

func TestHostLVM_AddTagToVG(t *testing.T) {
	tests := []struct {
		name    string
		vgName  string
		wantErr bool
		execErr bool
	}{
		{"Empty Volume Group Name", "", true, false},
		{"Error on Exec", "vg1", true, true},
		{"Tag added successfully", "vg1", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
				if tt.execErr {
					return fmt.Errorf("mocked error")
				}
				return nil
			}}

			err := NewHostLVM(executor).AddTagToVG(ctx, tt.vgName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostLVM_ListLVsByName(t *testing.T) {
	tests := []struct {
		name        string
		lvName      string
		vgName      string
		wantLVs     []string
		wantErr     bool
		execErr     bool
		shouldExist bool
	}{
		{"Empty Volume Group Name", "", "", []string{}, true, false, false},
		{"Error on Exec", "", "vg1", []string{}, true, true, false},
		{"LVs Exists", "lv1", "vg1", []string{"lv1", "lv2", "lv3"}, false, false, true},
		{"LVs Does not Exist", "imaginary", "vg1", []string{"lv1", "lv2", "lv3"}, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{MockRunCommandAsHostInto: func(ctx context.Context, into any, command string, args ...string) error {
				if tt.execErr {
					return fmt.Errorf("mocked error")
				}
				var lvs []LogicalVolume
				for i := range tt.wantLVs {
					lvs = append(lvs, LogicalVolume{Name: tt.wantLVs[i]})
				}
				data, err := json.Marshal(LVReport{Report: []LVReportItem{{Lv: lvs}}})
				assert.NoError(t, err)
				return json.Unmarshal(data, &into)
			}}

			exists, err := NewHostLVM(executor).LVExists(ctx, tt.lvName, tt.vgName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.shouldExist, exists)
		})
	}
}

func TestHostLVM_CreateLV(t *testing.T) {
	tests := []struct {
		name           string
		lvName         string
		vgName         string
		sizePercent    int
		chunkSizeBytes int64
		wantErr        bool
		execErr        bool
	}{
		{"Empty Volume Group Name", "lv1", "", 10, lvmv1alpha1.ChunkSizeDefault.Value(), true, false},
		{"Empty Logical Volume Name", "", "vg1", 10, lvmv1alpha1.ChunkSizeDefault.Value(), true, false},
		{"Invalid SizePercent", "lv1", "vg1", -10, lvmv1alpha1.ChunkSizeDefault.Value(), true, false},
		{"Error on Exec", "lv1", "vg1", 10, lvmv1alpha1.ChunkSizeDefault.Value(), true, true},
		{"LV created successfully", "lv1", "vg1", 10, lvmv1alpha1.ChunkSizeDefault.Value(), false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
				if tt.execErr {
					return fmt.Errorf("mocked error")
				}
				assert.ElementsMatch(t, args, []string{"-l", fmt.Sprintf("%d%%FREE", tt.sizePercent), "-c", fmt.Sprintf("%vb", tt.chunkSizeBytes), "-Z", "y", "-T", fmt.Sprintf("%s/%s", tt.vgName, tt.lvName)})
				return nil
			}}

			err := NewHostLVM(executor).CreateLV(ctx, tt.lvName, tt.vgName, tt.sizePercent, tt.chunkSizeBytes)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostLVM_ExtendLV(t *testing.T) {
	tests := []struct {
		name        string
		lvName      string
		vgName      string
		sizePercent int
		wantErr     bool
		execErr     bool
	}{
		{"Empty Volume Group Name", "lv1", "", 10, true, false},
		{"Empty Logical Volume Name", "", "vg1", 10, true, false},
		{"Invalid SizePercent", "lv1", "vg1", -10, true, false},
		{"Error on Exec", "lv1", "vg1", 10, true, true},
		{"LV extended successfully", "lv1", "vg1", 10, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
				if tt.execErr {
					return fmt.Errorf("mocked error")
				}

				assert.ElementsMatch(t, args, []string{"-l", fmt.Sprintf("%d%%Vg", tt.sizePercent), fmt.Sprintf("%s/%s", tt.vgName, tt.lvName)})
				return nil
			}}

			err := NewHostLVM(executor).ExtendLV(ctx, tt.lvName, tt.vgName, tt.sizePercent)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostLVM_DeleteLV(t *testing.T) {
	tests := []struct {
		name        string
		lvName      string
		vgName      string
		wantErr     bool
		lvChangeErr bool
		lvRemoveErr bool
	}{
		{"Empty Volume Group Name", "lv1", "", true, false, false},
		{"Empty Logical Volume Name", "", "vg1", true, false, false},
		{"Error on LV Deactivation", "lv1", "vg1", true, true, false},
		{"Error on LV Removal", "lv1", "vg1", true, false, true},
		{"LV deleted successfully", "lv1", "vg1", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
				switch command {
				case lvChangeCmd:
					if tt.lvChangeErr {
						return fmt.Errorf("mocked error")
					}
					assert.ElementsMatch(t, args, []string{"-an", fmt.Sprintf("%s/%s", tt.vgName, tt.lvName)})
				case lvRemoveCmd:
					if tt.lvRemoveErr {
						return fmt.Errorf("mocked error")
					}
					assert.ElementsMatch(t, args, []string{fmt.Sprintf("%s/%s", tt.vgName, tt.lvName)})
				}
				return nil
			}}

			err := NewHostLVM(executor).DeleteLV(ctx, tt.lvName, tt.vgName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostLVM_ActivateLV(t *testing.T) {
	tests := []struct {
		name        string
		lvName      string
		vgName      string
		wantErr     bool
		lvChangeErr bool
	}{
		{"Empty Volume Group Name", "lv1", "", true, false},
		{"Empty Logical Volume Name", "", "vg1", true, false},
		{"Error on LV Activation", "lv1", "vg1", true, true},
		{"LV activated successfully", "lv1", "vg1", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			executor := &test.MockExecutor{MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
				if tt.lvChangeErr {
					return fmt.Errorf("mocked error")
				}
				assert.ElementsMatch(t, args, []string{"-ay", fmt.Sprintf("%s/%s", tt.vgName, tt.lvName)})
				return nil
			}}

			err := NewHostLVM(executor).ActivateLV(ctx, tt.lvName, tt.vgName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewDefaultHostLVM(t *testing.T) {
	lvm := NewDefaultHostLVM()
	assert.NotNilf(t, lvm, "lvm should not be nil")
}

func Test_untaggedVGs(t *testing.T) {
	vgs := []VolumeGroup{
		{Name: "vg1", Tags: []string{"tag1"}},
		{Name: "vg2", Tags: []string{lvmsTag}},
	}

	vgs = untaggedVGs(vgs)

	assert.Len(t, vgs, 1)
	assert.Equal(t, "vg1", vgs[0].Name)
}
