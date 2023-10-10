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
	"encoding/json"
	"errors"
	"fmt"
	osexec "os/exec"
	"strings"

	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/exec"
)

type lvmError string

func (s lvmError) Error() string { return string(s) }

var (
	ErrVolumeGroupNotFound = lvmError("volume group not found")
)

const (
	DefaultChunkSize = "128"

	lvmCmd        = "/usr/sbin/lvm"
	vgCreateCmd   = "/usr/sbin/vgcreate"
	vgChangeCmd   = "/usr/sbin/vgchange"
	vgExtendCmd   = "/usr/sbin/vgextend"
	vgRemoveCmd   = "/usr/sbin/vgremove"
	pvRemoveCmd   = "/usr/sbin/pvremove"
	lvCreateCmd   = "/usr/sbin/lvcreate"
	lvExtendCmd   = "/usr/sbin/lvextend"
	lvRemoveCmd   = "/usr/sbin/lvremove"
	lvChangeCmd   = "/usr/sbin/lvchange"
	lvmDevicesCmd = "/usr/sbin/lvmdevices"

	lvmsTag = "@lvms"
)

// VGReport represents the output of the `vgs --reportformat json` command
type VGReport struct {
	Report []struct {
		Vg []struct {
			Name   string `json:"vg_name"`
			VgSize string `json:"vg_size"`
		} `json:"vg"`
	} `json:"report"`
}

// PVReport represents the output of the `pvs --reportformat json` command
type PVReport struct {
	Report []struct {
		Pv []PhysicalVolume `json:"pv"`
	} `json:"report"`
}

// LVReport represents the output of the `lvs --reportformat json` command
type LVReport struct {
	Report []LVReportItem `json:"report"`
}

type LVReportItem struct {
	Lv []LogicalVolume `json:"lv"`
}

type LogicalVolume struct {
	Name            string `json:"lv_name"`
	VgName          string `json:"vg_name"`
	PoolName        string `json:"pool_lv"`
	LvAttr          string `json:"lv_attr"`
	LvSize          string `json:"lv_size"`
	MetadataPercent string `json:"metadata_percent"`
}

type LVM interface {
	CreateVG(vg VolumeGroup) error
	ExtendVG(vg VolumeGroup, pvs []string) (VolumeGroup, error)
	AddTagToVG(vgName string) error
	DeleteVG(vg VolumeGroup) error
	GetVG(name string) (VolumeGroup, error)

	ListPVs(vgName string) ([]PhysicalVolume, error)
	ListVGs() ([]VolumeGroup, error)
	ListLVsByName(vgName string) ([]string, error)
	ListLVs(vgName string) (*LVReport, error)

	LVExists(lvName, vgName string) (bool, error)
	CreateLV(lvName, vgName string, sizePercent int) error
	ExtendLV(lvName, vgName string, sizePercent int) error
	ActivateLV(lvName, vgName string) error
	DeleteLV(lvName, vgName string) error
}

type HostLVM struct {
	exec.Executor
}

func NewDefaultHostLVM() *HostLVM {
	return NewHostLVM(&exec.CommandExecutor{})
}

func NewHostLVM(executor exec.Executor) *HostLVM {
	return &HostLVM{executor}
}

// VolumeGroup represents a volume group of linux lvm.
type VolumeGroup struct {
	// Name is the name of the volume group
	Name string `json:"vg_name"`

	// VgSize is the size of the volume group
	VgSize string `json:"vg_size"`

	// PVs is the list of physical volumes associated with the volume group
	PVs []PhysicalVolume `json:"pvs"`
}

// PhysicalVolume represents a physical volume of linux lvm.
type PhysicalVolume struct {
	// PvName is the name of the Physical Volume
	PvName string `json:"pv_name"`

	// UUID is the unique identifier of the Physical Volume used in the devices file
	UUID string `json:"pv_uuid"`

	// VgName is the name of the associated Volume Group, if any
	VgName string `json:"vg_name"`

	// PvFmt is the file format of the PhysicalVolume
	PvFmt string `json:"pv_fmt"`

	// PvAttr describes the attributes of the PhysicalVolume
	PvAttr string `json:"pv_attr"`

	// PvSize describes the total space of the PhysicalVolume
	PvSize string `json:"pv_size"`

	// PvFree describes the free space of the PhysicalVolume
	PvFree string `json:"pv_free"`

	// DevSize describes the size of the underlying device on which the PhysicalVolume was created
	DevSize string `json:"dev_size"`
}

// CreateVG creates a new volume group
func (hlvm *HostLVM) CreateVG(vg VolumeGroup) error {
	if vg.Name == "" {
		return fmt.Errorf("failed to create volume group. Volume group name is empty")
	}

	if len(vg.PVs) == 0 {
		return fmt.Errorf("failed to create volume group. Physical volume list is empty")
	}

	args := []string{vg.Name, "--addtag", lvmsTag}

	for _, pv := range vg.PVs {
		args = append(args, pv.PvName)
	}

	_, err := hlvm.ExecuteCommandWithOutputAsHost(vgCreateCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to create volume group %q. %v", vg.Name, err)
	}

	return nil
}

// ExtendVG Extend extends the volume group only if new physical volumes are available
func (hlvm *HostLVM) ExtendVG(vg VolumeGroup, pvs []string) (VolumeGroup, error) {
	if vg.Name == "" {
		return VolumeGroup{}, fmt.Errorf("failed to extend volume group. Volume group name is empty")
	}

	if len(pvs) == 0 {
		return VolumeGroup{}, fmt.Errorf("failed to extend volume group. Physical volume list is empty")
	}

	args := []string{vg.Name}
	args = append(args, pvs...)

	_, err := hlvm.ExecuteCommandWithOutputAsHost(vgExtendCmd, args...)
	if err != nil {
		return VolumeGroup{}, fmt.Errorf("failed to extend volume group %q. %v", vg.Name, err)
	}

	for _, pv := range pvs {
		vg.PVs = append(vg.PVs, PhysicalVolume{PvName: pv})
	}

	return vg, nil
}

// AddTagToVG adds a lvms tag to the volume group
func (hlvm *HostLVM) AddTagToVG(vgName string) error {
	if vgName == "" {
		return fmt.Errorf("failed to add tag to the volume group. Volume group name is empty")
	}

	args := []string{vgName, "--addtag", lvmsTag}

	_, err := hlvm.ExecuteCommandWithOutputAsHost(vgChangeCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to add tag to the volume group %q. %v", vgName, err)
	}

	return nil
}

// Delete deletes a volume group and the physical volumes associated with it
func (hlvm *HostLVM) DeleteVG(vg VolumeGroup) error {
	// Deactivate Volume Group
	vgArgs := []string{"-an", vg.Name}
	_, err := hlvm.ExecuteCommandWithOutputAsHost(vgChangeCmd, vgArgs...)
	if err != nil {
		return fmt.Errorf("failed to remove volume group %q: %w", vg.Name, err)
	}

	// Remove Volume Group
	vgArgs = []string{vg.Name}
	_, err = hlvm.ExecuteCommandWithOutputAsHost(vgRemoveCmd, vgArgs...)
	if err != nil {
		return fmt.Errorf("failed to remove volume group %q: %w", vg.Name, err)
	}

	// Remove physical volumes
	pvArgs := []string{}
	for _, pv := range vg.PVs {
		pvArgs = append(pvArgs, pv.PvName)
	}
	_, err = hlvm.ExecuteCommandWithOutputAsHost(pvRemoveCmd, pvArgs...)
	if err != nil {
		return fmt.Errorf("failed to remove physical volumes for the volume group %q: %w", vg.Name, err)
	}

	for _, pv := range vg.PVs {
		_, err = hlvm.ExecuteCommandWithOutput(lvmDevicesCmd, "--delpvid", pv.UUID)
		if err != nil {
			var exitError *osexec.ExitError
			if errors.As(err, &exitError) {
				switch exitError.ExitCode() {
				// Exit Code 5 On lvmdevices --delpvid means that the PV with that UUID no longer exists
				case 5:
					continue
				}
			}
			return fmt.Errorf("failed to delete PV %s from device file for the volume group %s: %w", pv.UUID, vg.Name, err)
		}
	}

	return nil
}

// GetVolumeGroup returns a volume group along with the associated physical volumes
func (hlvm *HostLVM) GetVG(name string) (VolumeGroup, error) {
	res := new(VGReport)

	args := []string{
		"vgs", lvmsTag, "--units", "g", "--reportformat", "json",
	}
	if err := hlvm.execute(res, args...); err != nil {
		return VolumeGroup{}, fmt.Errorf("failed to list volume groups. %v", err)
	}

	vgFound := false
	volumeGroup := VolumeGroup{}
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
		return VolumeGroup{}, ErrVolumeGroupNotFound
	}

	// Get Physical Volumes associated with the Volume Group
	pvs, err := hlvm.ListPVs(name)
	if err != nil {
		return VolumeGroup{}, fmt.Errorf("failed to list physical volumes for volume group %q. %v", name, err)
	}

	volumeGroup.PVs = pvs
	return volumeGroup, nil
}

// ListPhysicalVolumes returns list of physical volumes used to create the given volume group
func (hlvm *HostLVM) ListPVs(vgName string) ([]PhysicalVolume, error) {
	res := new(PVReport)
	args := []string{
		"pvs", "--units", "g", "-v", "--reportformat", "json",
	}
	if vgName != "" {
		args = append(args, "-S", fmt.Sprintf("vgname=%s", vgName))
	}
	if err := hlvm.execute(res, args...); err != nil {
		return []PhysicalVolume{}, err
	}

	var pvs []PhysicalVolume
	for _, report := range res.Report {
		for _, pv := range report.Pv {
			pvs = append(pvs, PhysicalVolume{
				PvName:  pv.PvName,
				UUID:    pv.UUID,
				VgName:  pv.VgName,
				PvFmt:   pv.PvFmt,
				PvAttr:  pv.PvAttr,
				PvSize:  pv.PvSize,
				PvFree:  pv.PvFree,
				DevSize: pv.DevSize,
			})
		}
	}
	return pvs, nil
}

// ListVolumeGroups lists all volume groups and the physical volumes associated with them.
func (hlvm *HostLVM) ListVGs() ([]VolumeGroup, error) {
	res := new(VGReport)

	if err := hlvm.execute(res, "vgs", lvmsTag, "--reportformat", "json"); err != nil {
		return nil, fmt.Errorf("failed to list volume groups. %v", err)
	}

	var vgList []VolumeGroup
	for _, report := range res.Report {
		for _, vg := range report.Vg {
			vgList = append(vgList, VolumeGroup{Name: vg.Name, PVs: []PhysicalVolume{}})
		}
	}

	// Get Physical Volumes associated with the Volume Group
	for i, vg := range vgList {
		pvs, err := hlvm.ListPVs(vg.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to list physical volumes for volume group %q. %v", vg.Name, err)
		}
		vgList[i].PVs = pvs
	}

	return vgList, nil
}

// ListLogicalVolumes returns list of logical volumes for a volume group
func (hlvm *HostLVM) ListLVsByName(vgName string) ([]string, error) {
	res, err := hlvm.ListLVs(vgName)
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

// LVReport returns the output for `lvs` command in json format
func (hlvm *HostLVM) ListLVs(vgName string) (*LVReport, error) {
	res := new(LVReport)
	args := []string{
		"lvs", "-S", fmt.Sprintf("vgname=%s", vgName), "--units", "g", "--reportformat", "json",
	}
	if err := hlvm.execute(res, args...); err != nil {
		return nil, err
	}

	return res, nil
}

// LVExists checks if a logical volume exists in a volume group
func (hlvm *HostLVM) LVExists(lvName, vgName string) (bool, error) {
	lvs, err := hlvm.ListLVsByName(vgName)
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
func (hlvm *HostLVM) DeleteLV(lvName, vgName string) error {
	lv := fmt.Sprintf("%s/%s", vgName, lvName)

	// deactivate logical volume
	_, err := hlvm.ExecuteCommandWithOutputAsHost(lvChangeCmd, "-an", lv)
	if err != nil {
		return fmt.Errorf("failed to deactivate thin pool %q in volume group %q. %w", lvName, vgName, err)
	}

	// delete logical volume
	_, err = hlvm.ExecuteCommandWithOutputAsHost(lvRemoveCmd, lv)
	if err != nil {
		return fmt.Errorf("failed to delete logical volume %q in volume group %q. %w", lvName, vgName, err)
	}

	return nil
}

// CreateLV creates the logical volume
func (hlvm *HostLVM) CreateLV(lvName, vgName string, sizePercent int) error {

	args := []string{"-l", fmt.Sprintf("%d%%FREE", sizePercent),
		"-c", DefaultChunkSize, "-Z", "y", "-T", fmt.Sprintf("%s/%s", vgName, lvName)}

	if _, err := hlvm.ExecuteCommandWithOutputAsHost(lvCreateCmd, args...); err != nil {
		return fmt.Errorf("failed to create logical volume %q in the volume group %q using command '%s': %w",
			lvName, vgName, fmt.Sprintf("%s %s", lvCreateCmd, strings.Join(args, " ")), err)
	}

	return nil
}

// ExtendLV extends the logical volume
func (hlvm *HostLVM) ExtendLV(lvName, vgName string, sizePercent int) error {

	args := []string{"-l", fmt.Sprintf("%d%%Vg", sizePercent), fmt.Sprintf("%s/%s", vgName, lvName)}

	if _, err := hlvm.ExecuteCommandWithOutputAsHost(lvExtendCmd, args...); err != nil {
		return fmt.Errorf("failed to extend logical volume %q in the volume group %q using command '%s': %w",
			lvName, vgName, fmt.Sprintf("%s %s", lvExtendCmd, strings.Join(args, " ")), err)
	}

	return nil
}

// ActivateLV activates the logical volume
func (hlvm *HostLVM) ActivateLV(lvName, vgName string) error {
	lv := fmt.Sprintf("%s/%s", vgName, lvName)

	// deactivate logical volume
	_, err := hlvm.ExecuteCommandWithOutputAsHost(lvChangeCmd, "-ay", lv)
	if err != nil {
		return fmt.Errorf("failed to activate thin pool %q in volume group %q. %w", lvName, vgName, err)
	}

	return nil
}

func (hlvm *HostLVM) execute(v interface{}, args ...string) error {
	output, err := hlvm.ExecuteCommandWithOutputAsHost(lvmCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to execute command. %v", err)
	}

	err = json.Unmarshal([]byte(output), &v)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response. %v", err)
	}

	return nil
}
