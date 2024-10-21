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
	"fmt"
	"strings"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ErrVolumeGroupNotFound = fmt.Errorf("volume group not found")
)

type ExitError interface {
	ExitCode() int
	error
}

const (
	vgsCmd        = "/usr/sbin/vgs"
	pvsCmd        = "/usr/sbin/pvs"
	lvsCmd        = "/usr/sbin/lvs"
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

	DefaultTag = "@lvms"
)

var (
	DefaultListLVColumns = []string{
		"lv_name",
		"vg_name",
		"pool_lv",
		"lv_attr",
		"lv_size",
		"metadata_percent",
		"chunk_size",
		"lv_metadata_size",
	}
)

// VGReport represents the output of the `vgs --reportformat json` command
type VGReport struct {
	Report []struct {
		Vg []struct {
			Name   string `json:"vg_name"`
			VgSize string `json:"vg_size"`
			Tags   string `json:"vg_tags"`
			VgAttr string `json:"vg_attr"`
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
	ChunkSize       string `json:"chunk_size"`
	MetadataSize    string `json:"lv_metadata_size"`
}

type SharedVGOptions struct {
	v1alpha1.DeviceAccessPolicy
	OwnsGlobalLockSpace func() bool
}

type CreateVGOptions struct {
	SharedVGOptions
}

type DeleteVGOptions struct {
	SharedVGOptions
}

type ListVGOptions struct {
	VGName string
}

type LVM interface {
	CreateVG(ctx context.Context, vg VolumeGroup, opts CreateVGOptions) error
	ExtendVG(ctx context.Context, vg VolumeGroup, pvs []string) (VolumeGroup, error)
	AddTagToVG(ctx context.Context, vgName string) error
	DeleteVG(ctx context.Context, vg VolumeGroup, opts DeleteVGOptions) error
	GetVG(ctx context.Context, name string) (VolumeGroup, error)

	ListPVs(ctx context.Context, vgName string) ([]PhysicalVolume, error)
	ListVGs(ctx context.Context, taggedByLVMS bool, opts ListVGOptions) ([]VolumeGroup, error)
	ListLVsByName(ctx context.Context, vgName string) ([]string, error)
	ListLVs(ctx context.Context, vgName string) (*LVReport, error)

	LVExists(ctx context.Context, lvName, vgName string) (bool, error)
	CreateLV(ctx context.Context, lvName, vgName string, sizePercent int, chunkSizeBytes, metadataSizeBytes int64) error
	ExtendLV(ctx context.Context, lvName, vgName string, sizePercent int) error
	ExtendThinPoolMetadata(ctx context.Context, lvName, vgName string, metadataSizeBytes int64) error
	ActivateLV(ctx context.Context, lvName, vgName string) error
	DeleteLV(ctx context.Context, lvName, vgName string) error

	LockStart(ctx context.Context, vgName string) error
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

	// Tags is the list of tags associated with the volume group
	Tags []string `json:"vg_tags"`

	VgAttr string `json:"vg_attr"`
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
func (hlvm *HostLVM) CreateVG(ctx context.Context, vg VolumeGroup, opts CreateVGOptions) error {
	if vg.Name == "" {
		return fmt.Errorf("failed to create volume group: volume group name is empty")
	}

	if len(vg.PVs) == 0 {
		return fmt.Errorf("failed to create volume group: physical volume list is empty")
	}

	args := []string{vg.Name, "--addtag", DefaultTag}

	if opts.DeviceAccessPolicy == v1alpha1.DeviceAccessPolicyShared {
		for _, pv := range vg.PVs {
			if err := hlvm.RunCommandAsHost(ctx, lvmDevicesCmd, "--adddev", pv.PvName); err != nil {
				return fmt.Errorf("failed to add PV %s to device file: %w", pv.UUID, err)
			}
		}

		if opts.OwnsGlobalLockSpace() {
			args = append(args, "--shared")
		} else {
			log.FromContext(ctx).Info("Skipping shared VG Creation as another node is the lockspace owner.")
			// If the global lockspace is not present (vg doesn't exist) and we are not the leader,
			// we need to skip the VG Creation
			return nil
		}
	}

	for _, pv := range vg.PVs {
		args = append(args, pv.PvName)
	}

	if err := hlvm.RunCommandAsHost(ctx, vgCreateCmd, args...); err != nil {
		return fmt.Errorf("failed to create volume group %q. %v", vg.Name, err)
	}

	return nil
}

// ExtendVG Extend extends the volume group only if new physical volumes are available
func (hlvm *HostLVM) ExtendVG(ctx context.Context, vg VolumeGroup, pvs []string) (VolumeGroup, error) {
	if vg.Name == "" {
		return VolumeGroup{}, fmt.Errorf("failed to extend volume group: volume group name is empty")
	}

	if len(pvs) == 0 {
		return VolumeGroup{}, fmt.Errorf("failed to extend volume group: physical volume list is empty")
	}

	args := []string{vg.Name}
	args = append(args, pvs...)

	if err := hlvm.RunCommandAsHost(ctx, vgExtendCmd, args...); err != nil {
		return VolumeGroup{}, fmt.Errorf("failed to extend volume group %q. %v", vg.Name, err)
	}

	for _, pv := range pvs {
		vg.PVs = append(vg.PVs, PhysicalVolume{PvName: pv})
	}

	return vg, nil
}

// AddTagToVG adds a lvms tag to the volume group
func (hlvm *HostLVM) AddTagToVG(ctx context.Context, vgName string) error {
	if vgName == "" {
		return fmt.Errorf("failed to add tag to the volume group. Volume group name is empty")
	}

	args := []string{vgName, "--addtag", DefaultTag}

	if err := hlvm.RunCommandAsHost(ctx, vgChangeCmd, args...); err != nil {
		return fmt.Errorf("failed to add tag to the volume group %q. %v", vgName, err)
	}

	return nil
}

// DeleteVG deletes a volume group and the physical volumes associated with it
func (hlvm *HostLVM) DeleteVG(ctx context.Context, vg VolumeGroup, opts DeleteVGOptions) error {
	// Deactivate Volume Group
	vgArgs := []string{"-an", vg.Name}
	if err := hlvm.RunCommandAsHost(ctx, vgChangeCmd, vgArgs...); err != nil {
		return fmt.Errorf("failed to remove volume group %q: %w", vg.Name, err)
	}

	if opts.DeviceAccessPolicy == v1alpha1.DeviceAccessPolicyShared && !opts.OwnsGlobalLockSpace() {
		if err := hlvm.RunCommandAsHost(ctx, vgChangeCmd, vg.Name, "--lock-stop"); err != nil {
			return fmt.Errorf("failed to stop lock for shared volume group %q: %w", vg.Name, err)
		}
		return nil
	}

	// Remove Volume Group
	vgArgs = []string{vg.Name}
	if err := hlvm.RunCommandAsHost(ctx, vgRemoveCmd, vgArgs...); err != nil {
		return fmt.Errorf("failed to remove volume group %q: %w", vg.Name, err)
	}

	// Remove physical volumes
	var pvArgs []string
	for _, pv := range vg.PVs {
		pvArgs = append(pvArgs, pv.PvName)
	}
	if err := hlvm.RunCommandAsHost(ctx, pvRemoveCmd, pvArgs...); err != nil {
		return fmt.Errorf("failed to remove physical volumes for the volume group %q: %w", vg.Name, err)
	}

	for _, pv := range vg.PVs {
		err := hlvm.RunCommandAsHost(ctx, lvmDevicesCmd, "--delpvid", pv.UUID)
		if err, ok := exec.AsExecError(err); ok && err.ExitCode() == 5 {
			// Exit Code 5 On lvmdevices --delpvid means that the PV with that UUID no longer exists
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to delete PV %s from device file for the volume group %s: %w", pv.UUID, vg.Name, err)
		}
	}

	return nil
}

// GetVG returns a volume group along with the associated physical volumes
func (hlvm *HostLVM) GetVG(ctx context.Context, name string) (VolumeGroup, error) {
	vgs, err := hlvm.ListVGs(ctx, false, ListVGOptions{
		VGName: name,
	})
	if len(vgs) == 0 {
		return VolumeGroup{}, ErrVolumeGroupNotFound
	}
	if len(vgs) > 1 {
		return VolumeGroup{}, fmt.Errorf("multiple volume groups found with the name %q", name)
	}
	if err, ok := exec.AsExecError(err); ok && strings.Contains(err.Error(),
		fmt.Sprintf("Volume group %q not found", name)) {
		return VolumeGroup{}, ErrVolumeGroupNotFound
	}
	return vgs[0], err
}

// ListPVs returns list of physical volumes used to create the given volume group
func (hlvm *HostLVM) ListPVs(ctx context.Context, vgName string) ([]PhysicalVolume, error) {
	res := new(PVReport)
	args := []string{
		"--units", "g", "-v", "--reportformat", "json",
	}
	if vgName != "" {
		args = append(args, "-S", fmt.Sprintf("vgname=%s", vgName))
	}
	if err := hlvm.RunCommandAsHostInto(ctx, res, pvsCmd, args...); err != nil {
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

// ListVGs lists all volume groups and the physical volumes associated with them.
func (hlvm *HostLVM) ListVGs(ctx context.Context, tagged bool, opts ListVGOptions) ([]VolumeGroup, error) {
	res := new(VGReport)

	args := []string{
		"-o", "vg_name,vg_size,vg_tags", "--units", "g", "--reportformat", "json",
	}

	if opts.VGName != "" {
		args = append(args, opts.VGName)
	}

	if tagged {
		args = append(args, DefaultTag)
	}

	if err := hlvm.RunCommandAsHostInto(ctx, res, vgsCmd, args...); err != nil {
		return nil, fmt.Errorf("failed to list volume groups. %v", err)
	}

	var vgList []VolumeGroup
	for _, report := range res.Report {
		for _, vg := range report.Vg {
			vgList = append(vgList, VolumeGroup{
				Name:   vg.Name,
				VgSize: vg.VgSize,
				PVs:    []PhysicalVolume{},
				Tags:   strings.Split(vg.Tags, ","),
				VgAttr: vg.VgAttr,
			})
		}
	}

	pvs, err := hlvm.ListPVs(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list physical volumes: %w", err)
	}

	// Get Physical Volumes associated with the Volume Group
	for i, vg := range vgList {
		var vgPvs []PhysicalVolume
		for _, pv := range pvs {
			if vg.Name == pv.VgName {
				vgPvs = append(vgPvs, pv)
			}
		}
		vgList[i].PVs = vgPvs
	}

	if !tagged {
		return untaggedVGs(vgList), nil
	}

	return vgList, nil
}

// ListLVsByName returns list of logical volumes for a volume group
func (hlvm *HostLVM) ListLVsByName(ctx context.Context, vgName string) ([]string, error) {
	if vgName == "" {
		return nil, fmt.Errorf("failed to list lvs by volume group: volume group name is empty")
	}

	res, err := hlvm.ListLVs(ctx, vgName)
	if err != nil {
		return []string{}, err
	}

	var lvs []string
	for _, report := range res.Report {
		for _, lv := range report.Lv {
			lvs = append(lvs, lv.Name)
		}
	}
	return lvs, nil
}

// ListLVs returns the output for `lvs` command in json format
func (hlvm *HostLVM) ListLVs(ctx context.Context, vgName string) (*LVReport, error) {
	res := new(LVReport)
	args := []string{
		"-S",
		fmt.Sprintf("vgname=%s", vgName),
		"--units",
		"b",
		"--nosuffix",
		"--reportformat",
		"json",
		"-o",
		strings.Join(DefaultListLVColumns, ","),
	}
	if err := hlvm.RunCommandAsHostInto(ctx, res, lvsCmd, args...); err != nil {
		return nil, err
	}

	return res, nil
}

// LVExists checks if a logical volume exists in a volume group
func (hlvm *HostLVM) LVExists(ctx context.Context, lvName, vgName string) (bool, error) {
	lvs, err := hlvm.ListLVsByName(ctx, vgName)
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
func (hlvm *HostLVM) DeleteLV(ctx context.Context, lvName, vgName string) error {
	if vgName == "" {
		return fmt.Errorf("failed to delete logical volume in volume group: volume group name is empty")
	}
	if lvName == "" {
		return fmt.Errorf("failed to delete logical volume in volume group: logical volume name is empty")
	}
	lv := fmt.Sprintf("%s/%s", vgName, lvName)

	// deactivate logical volume
	if err := hlvm.RunCommandAsHost(ctx, lvChangeCmd, "-an", lv); err != nil {
		return fmt.Errorf("failed to deactivate thin pool %q in volume group %q. %w", lvName, vgName, err)
	}

	// delete logical volume
	if err := hlvm.RunCommandAsHost(ctx, lvRemoveCmd, lv); err != nil {
		return fmt.Errorf("failed to delete logical volume %q in volume group %q. %w", lvName, vgName, err)
	}

	return nil
}

// CreateLV creates the logical volume
func (hlvm *HostLVM) CreateLV(ctx context.Context, lvName, vgName string, sizePercent int, chunkSizeBytes, metadataSizeBytes int64) error {
	if vgName == "" {
		return fmt.Errorf("failed to create logical volume in volume group: volume group name is empty")
	}
	if lvName == "" {
		return fmt.Errorf("failed to create logical volume in volume group: logical volume name is empty")
	}
	if sizePercent <= 0 {
		return fmt.Errorf("failed to create logical volume in volume group: size percent should be greater than 0")
	}

	args := []string{"-l", fmt.Sprintf("%d%%FREE", sizePercent), "-Z", "y", "-T"}

	if chunkSizeBytes > 0 {
		args = append(args, "-c", fmt.Sprintf("%vb", chunkSizeBytes))
	}

	if metadataSizeBytes > 0 {
		args = append(args, "--poolmetadatasize", fmt.Sprintf("%vb", metadataSizeBytes))
	}

	args = append(args, fmt.Sprintf("%s/%s", vgName, lvName))

	if err := hlvm.RunCommandAsHost(ctx, lvCreateCmd, args...); err != nil {
		return fmt.Errorf("failed to create logical volume %q in the volume group %q using command '%s': %w",
			lvName, vgName, fmt.Sprintf("%s %s", lvCreateCmd, strings.Join(args, " ")), err)
	}

	return nil
}

// ExtendLV extends the logical volume, sizePercent has to be calculated based on virtual gibibytes.
func (hlvm *HostLVM) ExtendLV(ctx context.Context, lvName, vgName string, sizePercent int) error {
	if vgName == "" {
		return fmt.Errorf("failed to extend logical volume in volume group: volume group name is empty")
	}
	if lvName == "" {
		return fmt.Errorf("failed to extend logical volume in volume group: logical volume name is empty")
	}
	if sizePercent <= 0 {
		return fmt.Errorf("failed to extend logical volume in volume group: size percent should be greater than 0")
	}

	args := []string{"-l", fmt.Sprintf("%d%%Vg", sizePercent), fmt.Sprintf("%s/%s", vgName, lvName)}

	if err := hlvm.RunCommandAsHost(ctx, lvExtendCmd, args...); err != nil {
		return fmt.Errorf("failed to extend logical volume %q in the volume group %q using command '%s': %w",
			lvName, vgName, fmt.Sprintf("%s %s", lvExtendCmd, strings.Join(args, " ")), err)
	}

	return nil
}

func (hlvm *HostLVM) ExtendThinPoolMetadata(ctx context.Context, lvName, vgName string, metadataSizeBytes int64) error {
	if vgName == "" {
		return fmt.Errorf("failed to extend logical volume metadata size in volume group: volume group name is empty")
	}
	if lvName == "" {
		return fmt.Errorf("failed to extend logical volume metadata size in volume group: logical volume name is empty")
	}
	if metadataSizeBytes <= 0 {
		return fmt.Errorf("failed to extend logical volume metadata size in volume group: size value should be greater than 0")
	}

	args := []string{"--poolmetadatasize", fmt.Sprintf("%vb", metadataSizeBytes), fmt.Sprintf("%s/%s", vgName, lvName)}
	err := hlvm.RunCommandAsHost(ctx, lvExtendCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to extend logical volume metadata size %q in the volume group %q using command '%s': %w",
			lvName, vgName, fmt.Sprintf("%s %s", lvExtendCmd, strings.Join(args, " ")), err)
	}
	return nil
}

// ActivateLV activates the logical volume
func (hlvm *HostLVM) ActivateLV(ctx context.Context, lvName, vgName string) error {
	if vgName == "" {
		return fmt.Errorf("failed to activate logical volume in volume group: volume group name is empty")
	}
	if lvName == "" {
		return fmt.Errorf("failed to activate logical volume in volume group: logical volume name is empty")
	}

	lv := fmt.Sprintf("%s/%s", vgName, lvName)

	// deactivate logical volume
	if err := hlvm.RunCommandAsHost(ctx, lvChangeCmd, "-ay", lv); err != nil {
		return fmt.Errorf("failed to activate thin pool %q in volume group %q. %w", lvName, vgName, err)
	}

	return nil
}

func (hlvm *HostLVM) LockStart(ctx context.Context, vgName string) error {
	if vgName == "" {
		return fmt.Errorf("failed to start lock for volume group: volume group name is empty")
	}

	if err := hlvm.RunCommandAsHost(ctx, vgChangeCmd, vgName, "--lockstart"); err != nil {
		return fmt.Errorf("failed to start lock for volume group %q: %w", vgName, err)
	}

	return nil
}

func untaggedVGs(vgs []VolumeGroup) []VolumeGroup {
	var untaggedVGs []VolumeGroup
	for _, vg := range vgs {
		tagPresent := false
		for _, tag := range vg.Tags {
			if tag == DefaultTag {
				tagPresent = true
				break
			}
		}
		if !tagPresent {
			untaggedVGs = append(untaggedVGs, vg)
		}
	}
	return untaggedVGs
}
