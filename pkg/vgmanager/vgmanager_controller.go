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
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/controllers"
	"github.com/openshift/lvm-operator/pkg/internal"
	"github.com/topolvm/topolvm/lvmd"
	lvmdCMD "github.com/topolvm/topolvm/pkg/lvmd/cmd"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	ControllerName            = "vg-manager"
	DefaultChunkSize          = "128"
	reconcileInterval         = 15 * time.Second
	metadataWarningPercentage = 95
)

var (
	reconcileAgain = ctrl.Result{Requeue: true, RequeueAfter: reconcileInterval}
)

// SetupWithManager sets up the controller with the Manager.
func (r *VGReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lvmv1alpha1.LVMVolumeGroup{}).
		Complete(r)
}

type VGReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	executor  internal.Executor
	NodeName  string
	Namespace string
}

func (r *VGReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling", "LVMVolumeGroup", req)

	// Check if this LVMVolumeGroup needs to be processed on this node
	volumeGroup := &lvmv1alpha1.LVMVolumeGroup{}
	if err := r.Client.Get(ctx, req.NamespacedName, volumeGroup); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the nodeSelector matches the labels on this node
	nodeMatches, err := r.matchesThisNode(ctx, volumeGroup.Spec.NodeSelector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to match nodeSelector to node labels: %w", err)
	}
	if !nodeMatches {
		// Nothing to be done on this node for the VG.
		logger.Info("node labels do not match the selector", "VGName", volumeGroup.Name)
		return ctrl.Result{}, nil
	}

	r.executor = &internal.CommandExecutor{}
	return r.reconcile(ctx, volumeGroup)
}

func (r *VGReconciler) reconcile(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	// Check if the LVMVolumeGroup resource is deleted
	if !volumeGroup.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.processDelete(ctx, volumeGroup)
	}

	// Read the lvmd config file
	lvmdConfig, err := loadLVMDConfig()
	if err != nil {
		err = fmt.Errorf("failed to read the lvmd config file: %w", err)
		if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
			logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
		}
		return ctrl.Result{}, err
	}
	if lvmdConfig == nil {
		// The lvmdconfig file does not exist and will be created.
		logger.Info("lvmd config file doesn't exist, will create")
		lvmdConfig = &lvmdCMD.Config{
			SocketName: controllers.DefaultLVMdSocket,
		}
	}
	existingLvmdConfig := *lvmdConfig

	vgs, err := ListVolumeGroups(r.executor)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list volume groups. %v", err)
	}

	blockDevices, err := internal.ListBlockDevices(r.executor)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list block devices: %v", err)
	}

	// Get the available block devices that can be used for this volume group
	availableDevices, err := r.getAvailableDevicesForVG(ctx, blockDevices, vgs, volumeGroup)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get block devices for volumegroup %s: %w", volumeGroup.GetName(), err)
	}

	logger.Info("listing available and delayed devices", "availableDevices", availableDevices)

	// If there are no available devices, that could mean either
	// - There is no available devices to attach to the volume group
	// - All the available devices are already attached
	if len(availableDevices) == 0 {
		devicesExist := false
		for _, vg := range vgs {
			if volumeGroup.Name == vg.Name {
				if len(vg.PVs) > 0 {
					devicesExist = true
				}
			}
		}

		if !devicesExist {
			err := fmt.Errorf("no available devices found for volume group %s", volumeGroup.GetName())
			if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
				logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
			}
			return ctrl.Result{}, err
		}

		// since the last reconciliation there could have been corruption on the LVs so we need to verify them again
		if err := r.validateLVs(ctx, volumeGroup); err != nil {
			err := fmt.Errorf("error while validating logical volumes in existing volume group: %w", err)
			if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
				logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
			}
			return ctrl.Result{}, err
		}

		logger.Info("all the available devices are attached to the volume group", "VGName", volumeGroup.Name)
		if err := r.setVolumeGroupReadyStatus(ctx, volumeGroup.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set status for volume group %s to ready: %w", volumeGroup.Name, err)
		}

		return reconcileAgain, nil
	}

	// Create/extend VG
	if err = r.addDevicesToVG(ctx, vgs, volumeGroup.Name, availableDevices); err != nil {
		err = fmt.Errorf("failed to create/extend volume group %s: %w", volumeGroup.Name, err)
		if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
			logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
		}
		return ctrl.Result{}, err
	}

	// Create thin pool
	if err = r.addThinPoolToVG(ctx, volumeGroup.Name, volumeGroup.Spec.ThinPoolConfig); err != nil {
		err := fmt.Errorf("failed to create thin pool %s for volume group %s: %w", volumeGroup.Spec.ThinPoolConfig.Name, volumeGroup.Name, err)
		if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
			logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
		}
		return ctrl.Result{}, err
	}

	// Validate the LVs created from the Thin-Pool to make sure the adding went as planned.
	if err := r.validateLVs(ctx, volumeGroup); err != nil {
		err := fmt.Errorf("error while validating logical volumes in existing volume group: %w", err)
		if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
			logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
		}
		return ctrl.Result{}, err
	}

	// Add the volume group to device classes inside lvmd config if not exists
	found := false
	for _, deviceClass := range lvmdConfig.DeviceClasses {
		if deviceClass.Name == volumeGroup.Name {
			found = true
		}
	}
	if !found {
		dc := &lvmd.DeviceClass{
			Name:           volumeGroup.Name,
			VolumeGroup:    volumeGroup.Name,
			Default:        volumeGroup.Spec.Default,
			ThinPoolConfig: &lvmd.ThinPoolConfig{},
		}

		if volumeGroup.Spec.ThinPoolConfig != nil {
			dc.Type = lvmd.TypeThin
			dc.ThinPoolConfig.Name = volumeGroup.Spec.ThinPoolConfig.Name
			dc.ThinPoolConfig.OverprovisionRatio = float64(volumeGroup.Spec.ThinPoolConfig.OverprovisionRatio)
		}

		lvmdConfig.DeviceClasses = append(lvmdConfig.DeviceClasses, dc)
	}

	// Apply and save lvmd config
	if !cmp.Equal(existingLvmdConfig, lvmdConfig) {
		if err := saveLVMDConfig(lvmdConfig); err != nil {
			err := fmt.Errorf("failed to update lvmd config file to update volume group %s: %w", volumeGroup.GetName(), err)
			if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
				logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
			}
			return ctrl.Result{}, err
		}
		logger.Info("updated lvmd config", "VGName", volumeGroup.Name)
	}

	if err := r.setVolumeGroupReadyStatus(ctx, volumeGroup.Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set status for volume group %s to ready: %w", volumeGroup.Name, err)
	}

	return reconcileAgain, nil
}

func (r *VGReconciler) processDelete(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup) error {
	logger := log.FromContext(ctx)

	// Read the lvmd config file
	lvmdConfig, err := loadLVMDConfig()
	if err != nil {
		// Failed to read lvmdconfig file. Reconcile again
		return fmt.Errorf("failed to read the lvmd config file: %w", err)
	}
	if lvmdConfig == nil {
		logger.Info("lvmd config file does not exist")
		// Remove the VG entry in the LVMVolumeGroupNodeStatus that was added to indicate the failures to the user.
		// This allows the LVMCluster to get deleted and not stuck/wait forever as LVMCluster looks for the LVMVolumeGroupNodeStatus before deleting.
		if err := r.removeVolumeGroupStatus(ctx, volumeGroup.Name); err != nil {
			return fmt.Errorf("failed to remove status for volume group %s: %w", volumeGroup.Name, err)
		}
		return nil
	}
	// To avoid having to iterate through device classes multiple times, map from name to config index
	deviceClassMap := make(map[string]int)
	for i, deviceClass := range lvmdConfig.DeviceClasses {
		deviceClassMap[deviceClass.Name] = i
	}
	index, found := deviceClassMap[volumeGroup.Name]
	if !found {
		// Nothing to do here.
		logger.Info("could not find volume group in lvmd deviceclasses list", "VGName", volumeGroup.Name)
		if err := r.removeVolumeGroupStatus(ctx, volumeGroup.Name); err != nil {
			return fmt.Errorf("failed to remove status for volume group %s: %w", volumeGroup.Name, err)
		}
		return nil
	}

	// Check if volume group exists
	vg, err := GetVolumeGroup(r.executor, volumeGroup.Name)
	if err != nil {
		if err != ErrVolumeGroupNotFound {
			return fmt.Errorf("failed to get volume group %s, %w", volumeGroup.GetName(), err)
		}
		return nil
	}

	// Delete thin pool
	if volumeGroup.Spec.ThinPoolConfig != nil {
		thinPoolName := volumeGroup.Spec.ThinPoolConfig.Name
		lvExists, err := LVExists(r.executor, thinPoolName, volumeGroup.Name)
		if err != nil {
			return fmt.Errorf("failed to check existence of thin pool %q in volume group %q. %v", thinPoolName, volumeGroup.Name, err)
		}

		if lvExists {
			if err := DeleteLV(r.executor, thinPoolName, volumeGroup.Name); err != nil {
				err := fmt.Errorf("failed to delete thin pool %s in volume group %s: %w", thinPoolName, volumeGroup.Name, err)
				if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
					logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
				}
				return err
			}
			logger.Info("thin pool deleted in the volume group", "VGName", volumeGroup.Name, "ThinPool", thinPoolName)
		} else {
			logger.Info("thin pool not found in the volume group", "VGName", volumeGroup.Name, "ThinPool", thinPoolName)
		}
	}

	if err = vg.Delete(r.executor); err != nil {
		err := fmt.Errorf("failed to delete volume group %s: %w", volumeGroup.Name, err)
		if err := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, err); err != nil {
			logger.Error(err, "failed to set status to failed", "VGName", volumeGroup.GetName())
		}
		return err
	}

	// Remove this vg from the lvmdconf file
	lvmdConfig.DeviceClasses = append(lvmdConfig.DeviceClasses[:index], lvmdConfig.DeviceClasses[index+1:]...)

	logger.Info("updating lvmd config")
	if len(lvmdConfig.DeviceClasses) > 0 {
		if err = saveLVMDConfig(lvmdConfig); err != nil {
			return fmt.Errorf("failed to update lvmd.conf file for volume group %s: %w", volumeGroup.GetName(), err)
		}
	} else {
		if err = deleteLVMDConfig(); err != nil {
			return fmt.Errorf("failed to delete lvmd.conf file for volume group %s: %w", volumeGroup.GetName(), err)
		}
	}

	if err := r.removeVolumeGroupStatus(ctx, volumeGroup.Name); err != nil {
		return fmt.Errorf("failed to remove status for volume group %s: %w", volumeGroup.Name, err)
	}

	return nil
}

// validateLVs verifies that all lvs that should have been created in the volume group are present and
// in their correct state
func (r *VGReconciler) validateLVs(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup) error {
	logger := log.FromContext(ctx)

	// If we don't have a ThinPool, VG Manager has no authority about the top Level LVs inside the VG, but TopoLVM
	if volumeGroup.Spec.ThinPoolConfig == nil {
		return nil
	}

	resp, err := GetLVSOutput(r.executor, volumeGroup.Name)
	if err != nil {
		return fmt.Errorf("could not get logical volumes found inside volume group, volume group content is degraded or corrupt: %w", err)
	}
	if len(resp.Report) < 1 {
		return fmt.Errorf("LV report was empty, meaning that the thin-pool LV is no longer found, " +
			"but the volume group might still exist")
	}

	for _, report := range resp.Report {
		if len(report.Lv) < 1 {
			return fmt.Errorf("no LV was found in the report, meaning that the thin-pool LV is no longer found, " +
				"but the volume group might still exist")
		}
		thinPoolExists := false
		for _, lv := range report.Lv {
			if lv.Name != volumeGroup.Spec.ThinPoolConfig.Name {
				continue
			}
			thinPoolExists = true
			lvAttr, err := ParsedLvAttr(lv.LvAttr)
			if err != nil {
				return fmt.Errorf("could not parse lv_attr from logical volume %s: %w", lv.Name, err)
			}
			if lvAttr.VolumeType != VolumeTypeThinPool {
				return fmt.Errorf("found logical volume in volume group that is not of type Thin-Pool, "+
					"even though there is a Thin-Pool configured: %s, lv_attr: %s,"+
					"this is most likely a corruption of the thin pool or a setup gone wrong",
					string(lvAttr.VolumeType), lvAttr)
			}

			if lvAttr.State != StateActive {
				return fmt.Errorf("found inactive logical volume, maybe external repairs are necessary/already happening or there is another"+
					"entity conflicting with vg-manager, cannot proceed until volume is activated again: lv_attr: %s", lvAttr)
			}
			metadataPercentage, err := strconv.ParseFloat(lv.MetadataPercent, 32)
			if err != nil {
				return fmt.Errorf("could not ensure metadata percentage of LV due to a parsing error: %w", err)
			}
			if metadataPercentage > metadataWarningPercentage {
				return fmt.Errorf("metadata partition is over %v percent filled and LVM Metadata Overflows cannot be recovered"+
					"you should manually extend the metadata_partition or you will risk data loss: metadata_percent: %v", metadataPercentage, lv.MetadataPercent)
			}

			logger.Info("confirmed created logical volume has correct attributes", "lv_attr", lvAttr.String())
		}
		if !thinPoolExists {
			return fmt.Errorf("the thin-pool LV is no longer present, but the volume group might still exist")
		}
	}
	return nil
}

func (r *VGReconciler) addThinPoolToVG(ctx context.Context, vgName string, config *lvmv1alpha1.ThinPoolConfig) error {
	logger := log.FromContext(ctx)

	resp, err := GetLVSOutput(r.executor, vgName)
	if err != nil {
		return fmt.Errorf("failed to list logical volumes in the volume group %q. %v", vgName, err)
	}

	for _, report := range resp.Report {
		for _, lv := range report.Lv {
			if lv.Name == config.Name {
				if strings.Contains(lv.LvAttr, "t") {
					logger.Info("lvm thinpool already exists", "VGName", vgName, "ThinPool", config.Name)
					if err := r.extendThinPool(ctx, vgName, lv.LvSize, config); err != nil {
						return fmt.Errorf("failed to extend the lvm thinpool %s in volume group %s: %w", config.Name, vgName, err)
					}
					return nil
				}

				return fmt.Errorf("failed to create thin pool %q, logical volume with same name already exists", config.Name)
			}
		}
	}

	args := []string{"-l", fmt.Sprintf("%d%%FREE", config.SizePercent), "-c", DefaultChunkSize, "-Z", "y", "-T", fmt.Sprintf("%s/%s", vgName, config.Name)}

	if _, err = r.executor.ExecuteCommandWithOutputAsHost(lvCreateCmd, args...); err != nil {
		return fmt.Errorf("failed to create thin pool %q in the volume group %q using command '%s': %v", config.Name, vgName, fmt.Sprintf("%s %s", lvCreateCmd, strings.Join(args, " ")), err)
	}

	return nil
}

func (r *VGReconciler) extendThinPool(ctx context.Context, vgName string, lvSize string, config *lvmv1alpha1.ThinPoolConfig) error {
	logger := log.FromContext(ctx)

	vg, err := GetVolumeGroup(r.executor, vgName)
	if err != nil {
		if err != ErrVolumeGroupNotFound {
			return fmt.Errorf("failed to get volume group. %q, %v", vgName, err)
		}
		return nil
	}

	thinPoolSize, err := strconv.ParseFloat(lvSize[:len(lvSize)-1], 64)
	if err != nil {
		return fmt.Errorf("failed to parse lvSize. %v", err)
	}

	vgSize, err := strconv.ParseFloat(vg.VgSize[:len(vg.VgSize)-1], 64)
	if err != nil {
		return fmt.Errorf("failed to parse vgSize. %v", err)
	}

	// return if thinPoolSize does not require expansion
	if config.SizePercent <= int((thinPoolSize/vgSize)*100) {
		return nil
	}

	logger.Info("extending lvm thinpool ", "VGName", vgName, "ThinPool", config.Name)

	args := []string{"-l", fmt.Sprintf("%d%%Vg", config.SizePercent), fmt.Sprintf("%s/%s", vgName, config.Name)}

	if _, err = r.executor.ExecuteCommandWithOutputAsHost(lvExtendCmd, args...); err != nil {
		return fmt.Errorf("failed to extend thin pool %q in the volume group %q using command '%s': %v", config.Name, vgName, fmt.Sprintf("%s %s", lvExtendCmd, strings.Join(args, " ")), err)
	}

	logger.Info("successfully extended the thin pool in the volume group ", "thinpool", config.Name, "vgName", vgName)

	return nil
}

func NodeSelectorMatchesNodeLabels(node *corev1.Node, nodeSelector *corev1.NodeSelector) (bool, error) {
	if nodeSelector == nil {
		return true, nil
	}
	if node == nil {
		return false, fmt.Errorf("node cannot be nil")
	}

	matches, err := corev1helper.MatchNodeSelectorTerms(node, nodeSelector)
	return matches, err
}

func (r *VGReconciler) matchesThisNode(ctx context.Context, selector *corev1.NodeSelector) (bool, error) {
	node := &corev1.Node{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: r.NodeName}, node)
	if err != nil {
		return false, err
	}
	return NodeSelectorMatchesNodeLabels(node, selector)
}

func loadLVMDConfig() (*lvmdCMD.Config, error) {

	cfgBytes, err := os.ReadFile(controllers.LvmdConfigFile)
	if os.IsNotExist(err) {
		// If the file does not exist, return nil for both
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to load config file %s: %w", controllers.LvmdConfigFile, err)
	} else {
		lvmdconfig := &lvmdCMD.Config{}
		if err = yaml.Unmarshal(cfgBytes, lvmdconfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config file %s: %w", controllers.LvmdConfigFile, err)
		}
		return lvmdconfig, nil
	}
}

func saveLVMDConfig(lvmdConfig *lvmdCMD.Config) error {
	out, err := yaml.Marshal(lvmdConfig)
	if err == nil {
		err = os.WriteFile(controllers.LvmdConfigFile, out, 0600)
	}
	if err != nil {
		return fmt.Errorf("failed to save config file %s: %w", controllers.LvmdConfigFile, err)
	}
	return nil
}

func deleteLVMDConfig() error {
	err := os.Remove(controllers.LvmdConfigFile)
	if err != nil {
		return fmt.Errorf("failed to delete config file %s: %w", controllers.LvmdConfigFile, err)
	}
	return err
}
