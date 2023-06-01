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
	"encoding/json"
	"fmt"

	"github.com/openshift/lvm-operator/pkg/internal"
)

type lvmError string

func (s lvmError) Error() string { return string(s) }

var (
	ErrVolumeGroupNotFound = lvmError("volume group not found")
)

const (
	lvmCmd      = "/usr/sbin/lvm"
	vgCreateCmd = "/usr/sbin/vgcreate"
	vgExtendCmd = "/usr/sbin/vgextend"
	vgRemoveCmd = "/usr/sbin/vgremove"
	pvRemoveCmd = "/usr/sbin/pvremove"
	lvCreateCmd = "/usr/sbin/lvcreate"
	lvExtendCmd = "/usr/sbin/lvextend"
	lvRemoveCmd = "/usr/sbin/lvremove"
	lvChangeCmd = "/usr/sbin/lvchange"
)

// vgsOutput represents the output of the `vgs --reportformat json` command
type vgsOutput struct {
	Report []struct {
		Vg []struct {
			Name   string `json:"vg_name"`
			VgSize string `json:"vg_size"`
		} `json:"vg"`
	} `json:"report"`
}

// pvsOutput represents the output of the `pvs --reportformat json` command
type pvsOutput struct {
	Report []struct {
		Pv []struct {
			Name   string `json:"pv_name"`
			VgName string `json:"vg_name"`
		} `json:"pv"`
	} `json:"report"`
}

// lvsOutput represents the output of the `lvs --reportformat json` command
type lvsOutput struct {
	Report []struct {
		Lv []struct {
			Name     string `json:"lv_name"`
			VgName   string `json:"vg_name"`
			PoolName string `json:"pool_lv"`
			LvAttr   string `json:"lv_attr"`
			LvSize   string `json:"lv_size"`
		} `json:"lv"`
	} `json:"report"`
}

// VolumeGroup represents a volume group of linux lvm.
type VolumeGroup struct {
	// Name is the name of the volume group
	Name string `json:"vg_name"`

	// VgSize is the size of the volume group
	VgSize string `json:"vg_size"`

	// PVs is the list of physical volumes associated with the volume group
	PVs []string `json:"pvs"`
}

// Create creates a new volume group
func (vg VolumeGroup) Create(exec internal.Executor, pvs []string) error {
	if vg.Name == "" {
		return fmt.Errorf("failed to create volume group. Volume group name is empty")
	}

	if len(pvs) == 0 {
		return fmt.Errorf("failed to create volume group. Physical volume list is empty")
	}

	args := []string{vg.Name}
	args = append(args, pvs...)

	_, err := exec.ExecuteCommandWithOutputAsHost(vgCreateCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to create volume group %q. %v", vg.Name, err)
	}

	return nil
}

// Extend extends the volume group only if new physical volumes are available
func (vg VolumeGroup) Extend(exec internal.Executor, pvs []string) error {
	if vg.Name == "" {
		return fmt.Errorf("failed to extend volume group. Volume group name is empty")
	}

	if len(pvs) == 0 {
		return fmt.Errorf("failed to extend volume group. Physical volume list is empty")
	}

	args := []string{vg.Name}
	args = append(args, pvs...)

	_, err := exec.ExecuteCommandWithOutputAsHost(vgExtendCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to extend volume group %q. %v", vg.Name, err)
	}

	return nil
}

// Delete deletes a volume group and the physical volumes associated with it
func (vg VolumeGroup) Delete(exec internal.Executor) error {
	// Remove Volume Group
	vgArgs := []string{vg.Name}
	_, err := exec.ExecuteCommandWithOutputAsHost(vgRemoveCmd, vgArgs...)
	if err != nil {
		return fmt.Errorf("failed to remove volume group %q. %v", vg.Name, err)
	}

	// Remove physical volumes
	pvArgs := vg.PVs
	_, err = exec.ExecuteCommandWithOutputAsHost(pvRemoveCmd, pvArgs...)
	if err != nil {
		return fmt.Errorf("failed to remove physical volumes for the volume group %q. %v", vg.Name, err)
	}
	return nil
}

// GetVolumeGroup returns a volume group along with the associated physical volumes
func GetVolumeGroup(exec internal.Executor, name string) (*VolumeGroup, error) {
	res := new(vgsOutput)

	args := []string{
		"vgs", "--units", "g", "--reportformat", "json",
	}
	if err := execute(exec, res, args...); err != nil {
		return nil, fmt.Errorf("failed to list volume groups. %v", err)
	}

	vgFound := false
	volumeGroup := &VolumeGroup{}
	for _, report := range res.Report {
		for _, vg := range report.Vg {
			if vg.Name == name {
				volumeGroup.Name = vg.Name
				volumeGroup.VgSize = vg.VgSize
				vgFound = true
				break
			}
		}
	}

	if !vgFound {
		return nil, ErrVolumeGroupNotFound
	}

	// Get Physical Volumes associated with the Volume Group
	pvs, err := ListPhysicalVolumes(exec, name)
	if err != nil {
		return nil, fmt.Errorf("failed to list physical volumes for volume group %q. %v", name, err)
	}

	volumeGroup.PVs = pvs
	return volumeGroup, nil
}

// ListPhysicalVolumes returns list of physical volumes used to create the given volume group
func ListPhysicalVolumes(exec internal.Executor, vgName string) ([]string, error) {
	res := new(pvsOutput)
	args := []string{
		"pvs", "-S", fmt.Sprintf("vgname=%s", vgName), "--reportformat", "json",
	}
	if err := execute(exec, res, args...); err != nil {
		return []string{}, err
	}

	pvs := []string{}
	for _, report := range res.Report {
		for _, pv := range report.Pv {
			pvs = append(pvs, pv.Name)
		}
	}
	return pvs, nil
}

// ListVolumeGroups lists all volume groups and the physical volumes associated with them.
func ListVolumeGroups(exec internal.Executor) ([]VolumeGroup, error) {
	res := new(vgsOutput)
	args := []string{
		"vgs", "--reportformat", "json",
	}

	if err := execute(exec, res, args...); err != nil {
		return nil, fmt.Errorf("failed to list volume groups. %v", err)
	}

	vgList := []VolumeGroup{}
	for _, report := range res.Report {
		for _, vg := range report.Vg {
			vgList = append(vgList, VolumeGroup{Name: vg.Name, PVs: []string{}})
		}
	}

	// Get Physical Volumes associated with the Volume Group
	for i, vg := range vgList {
		pvs, err := ListPhysicalVolumes(exec, vg.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to list physical volumes for volume group %q. %v", vg.Name, err)
		}
		vgList[i].PVs = pvs
	}

	return vgList, nil
}

// ListLogicalVolumes returns list of logical volumes for a volume group
func ListLogicalVolumes(exec internal.Executor, vgName string) ([]string, error) {
	res, err := GetLVSOutput(exec, vgName)
	if err != nil {
		return []string{}, err
	}

	lvs := []string{}
	for _, report := range res.Report {
		for _, lv := range report.Lv {
			lvs = append(lvs, lv.Name)
		}
	}
	return lvs, nil
}

// GetLVSOutput returns the output for `lvs` command in json format
func GetLVSOutput(exec internal.Executor, vgName string) (*lvsOutput, error) {
	res := new(lvsOutput)
	args := []string{
		"lvs", "-S", fmt.Sprintf("vgname=%s", vgName), "--units", "g", "--reportformat", "json",
	}
	if err := execute(exec, res, args...); err != nil {
		return nil, err
	}

	return res, nil
}

// LVExists checks if a logical volume exists in a volume group
func LVExists(exec internal.Executor, lvName, vgName string) (bool, error) {
	lvs, err := ListLogicalVolumes(exec, vgName)
	if err != nil {
		return false, err
	}

	for _, lv := range lvs {
		if lv == lvName {
			return true, nil
		}
	}

	return false, nil
}

// DeleteLV deactivates the logical volume and deletes it
func DeleteLV(exec internal.Executor, lvName, vgName string) error {
	lv := fmt.Sprintf("%s/%s", vgName, lvName)

	// deactivate logical volume
	_, err := exec.ExecuteCommandWithOutputAsHost(lvChangeCmd, "-an", lv)
	if err != nil {
		return fmt.Errorf("failed to deactivate thin pool %q in volume group %q. %v", lvName, vgName, err)
	}

	// delete logical volume
	_, err = exec.ExecuteCommandWithOutputAsHost(lvRemoveCmd, lv)
	if err != nil {
		return fmt.Errorf("failed to delete logical volume %q in volume group %q. %v", lvName, vgName, err)
	}

	return nil
}

func execute(exec internal.Executor, v interface{}, args ...string) error {
	output, err := exec.ExecuteCommandWithOutputAsHost(lvmCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to execute command. %v", err)
	}

	err = json.Unmarshal([]byte(output), &v)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response. %v", err)
	}

	return nil
}
