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

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/controllers"
	"github.com/openshift/lvm-operator/pkg/internal"
	"github.com/topolvm/topolvm/lvmd"
	lvmdCMD "github.com/topolvm/topolvm/pkg/lvmd/cmd"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	ControllerName    = "vg-manager"
	DefaultChunkSize  = "128"
	reconcileInterval = 1 * time.Minute
)

var (
	reconcileAgain = ctrl.Result{Requeue: true, RequeueAfter: reconcileInterval}
)

// SetupWithManager sets up the controller with the Manager.
func (r *VGReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.deviceAgeMap = newAgeMap(&wallTime{})
	return ctrl.NewControllerManagedBy(mgr).
		For(&lvmv1alpha1.LVMVolumeGroup{}).
		Complete(r)
}

type VGReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	// map from KNAME of device to time when the device was first observed since the process started
	deviceAgeMap *ageMap
	executor     internal.Executor
	NodeName     string
	Namespace    string
}

func (r *VGReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log = log.FromContext(ctx).WithName(ControllerName)
	r.Log.Info("reconciling", "LVMVolumeGroup", req)

	// Check if this LVMVolumeGroup needs to be processed on this node
	volumeGroup := &lvmv1alpha1.LVMVolumeGroup{}
	err := r.Client.Get(ctx, req.NamespacedName, volumeGroup)
	if err != nil {
		r.Log.Error(err, "failed to get LVMVolumeGroup", "VGName", req.Name)
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return reconcileAgain, err
	}
	// Check if the nodeSelector matches the labels on this node
	nodeMatches, err := r.matchesThisNode(ctx, volumeGroup.Spec.NodeSelector)
	if err != nil {
		r.Log.Error(err, "failed to match nodeSelector to node labels", "VGName", volumeGroup.Name)
		return ctrl.Result{}, err
	}
	if !nodeMatches {
		// Nothing to be done on this node for the VG.
		r.Log.Info("node labels do not match the selector", "VGName", volumeGroup.Name)
		return ctrl.Result{}, nil
	}

	r.executor = &internal.CommandExecutor{}
	res, err := r.reconcile(ctx, volumeGroup)
	if err != nil {
		r.Log.Error(err, "reconcile error", "LVMVolumeGroup", volumeGroup.Name)
	}
	r.Log.Info("reconcile complete", "result", res)
	return res, err
}

func (r *VGReconciler) reconcile(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup) (ctrl.Result, error) {
	// Check if the LVMVolumeGroup resource is deleted
	if !volumeGroup.DeletionTimestamp.IsZero() {
		err := r.processDelete(ctx, volumeGroup)
		return ctrl.Result{}, err
	}

	// Read the lvmd config file
	lvmdConfig, err := loadLVMDConfig()
	if err != nil {
		r.Log.Error(err, "failed to read the lvmd config file")
		if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, fmt.Sprintf("failed to read the lvmd config file: %v", err.Error())); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "name", volumeGroup.Name)
		}
		return reconcileAgain, err
	}
	if lvmdConfig == nil {
		// The lvmdconfig file does not exist and will be created.
		r.Log.Info("lvmd config file doesn't exist, will create")
		lvmdConfig = &lvmdCMD.Config{
			SocketName: controllers.DefaultLVMdSocket,
		}
	}
	existingLvmdConfig := *lvmdConfig

	vgs, err := ListVolumeGroups(r.executor)
	if err != nil {
		return reconcileAgain, fmt.Errorf("failed to list volume groups. %v", err)
	}

	blockDevices, err := internal.ListBlockDevices(r.executor)
	if err != nil {
		return reconcileAgain, fmt.Errorf("failed to list block devices: %v", err)
	}

	//Get the available block devices that can be used for this volume group
	availableDevices, delayedDevices, err := r.getAvailableDevicesForVG(blockDevices, vgs, volumeGroup)
	if err != nil {
		r.Log.Error(err, "failed to get block devices for volumegroup, will retry", "name", volumeGroup.Name)
		// Set a failure status only if there is an error and there is no delayed devices. If there are delayed devices, there is a chance that this will pass in the next reconciliation.
		if len(delayedDevices) == 0 {
			if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, fmt.Sprintf("failed to get block devices for volumegroup %s: %v", volumeGroup.Name, err.Error())); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "name", volumeGroup.Name)
			}
		}

		// Failed to get devices for this volume group. Reconcile again.
		return reconcileAgain, err
	}

	r.Log.Info("listing available and delayed devices", "availableDevices", availableDevices, "delayedDevices", delayedDevices)

	// If there are no available devices, that could mean either
	// - There is no available devices to attach to the volume group
	// - All the available devices are already attached
	if len(availableDevices) == 0 {
		if len(delayedDevices) > 0 {
			r.Log.Info("there are delayed devices, will retry them in the next reconciliation", "VGName", volumeGroup.Name, "delayedDevices", delayedDevices)
			if statuserr := r.setVolumeGroupProgressingStatus(ctx, volumeGroup.Name); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
				return reconcileAgain, statuserr
			}
			return ctrl.Result{Requeue: true, RequeueAfter: 30 * time.Second}, nil //30 seconds to make sure delayed devices become available
		}

		devicesExist := false
		for _, vg := range vgs {
			if volumeGroup.Name == vg.Name {
				if len(vg.PVs) > 0 {
					devicesExist = true
				}
			}
		}

		if devicesExist {
			r.Log.Info("all the available devices are attached to the volume group", "VGName", volumeGroup.Name)
			if statuserr := r.setVolumeGroupReadyStatus(ctx, volumeGroup.Name); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
				return reconcileAgain, statuserr
			}
		} else {
			errMsg := "no available devices found for volume group"
			r.Log.Error(fmt.Errorf(errMsg), errMsg, "VGName", volumeGroup.Name)
			if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, errMsg); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "name", volumeGroup.Name)
				return reconcileAgain, statuserr
			}
		}

		return reconcileAgain, nil
	}

	// Create/extend VG
	err = r.addDevicesToVG(vgs, volumeGroup.Name, availableDevices)
	if err != nil {
		r.Log.Error(err, "failed to create/extend volume group", "VGName", volumeGroup.Name)
		if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, fmt.Sprintf("failed to create/extend volume group %s: %v", volumeGroup.Name, err.Error())); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
		}
		return reconcileAgain, err
	}

	if volumeGroup.Spec.RAIDConfig == nil {
		// Create thin pool
		err = r.setupThinPool(volumeGroup.Name, volumeGroup.Spec.ThinPoolConfig)
		if err != nil {
			r.Log.Error(err, "failed to create thin pool", "VGName", "ThinPool", volumeGroup.Name, volumeGroup.Spec.ThinPoolConfig.Name)
			if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name,
				fmt.Sprintf("failed to create thin pool %s for volume group %s: %v", volumeGroup.Spec.ThinPoolConfig.Name, volumeGroup.Name, err.Error())); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
				return reconcileAgain, statuserr
			}
		}
	} else {
		err = r.setupRAIDThinPool(volumeGroup)
		if err != nil {
			r.Log.Error(err, "failed to create thin pool (with RAID)", "VGName", "ThinPool", volumeGroup.Name, volumeGroup.Spec.ThinPoolConfig.Name)
			if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name,
				fmt.Sprintf("failed to create thin pool %s for volume group %s: %v", volumeGroup.Spec.ThinPoolConfig.Name, volumeGroup.Name, err.Error())); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
				return reconcileAgain, statuserr
			}
		}
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
		err := saveLVMDConfig(lvmdConfig)
		if err != nil {
			r.Log.Error(err, "failed to update lvmd config file", "VGName", volumeGroup.Name)
			if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, fmt.Sprintf("failed to update lvmd config file: %v", err.Error())); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "name", volumeGroup.Name)
			}
			return reconcileAgain, err
		}
		r.Log.Info("updated lvmd config", "VGName", volumeGroup.Name)
	}

	if statuserr := r.setVolumeGroupReadyStatus(ctx, volumeGroup.Name); statuserr != nil {
		r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
		return reconcileAgain, nil
	}

	// requeue faster if some devices are too recently observed to consume
	requeueAfter := time.Minute * 1
	if len(delayedDevices) > 0 {
		requeueAfter = time.Second * 30
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *VGReconciler) processDelete(ctx context.Context, volumeGroup *lvmv1alpha1.LVMVolumeGroup) error {

	// Read the lvmd config file
	lvmdConfig, err := loadLVMDConfig()
	if err != nil {
		// Failed to read lvmdconfig file. Reconcile again
		r.Log.Error(err, "failed to read the lvmd config file")
		return err
	}
	if lvmdConfig == nil {
		r.Log.Info("lvmd config file does not exist")
		// Remove the VG entry in the LVMVolumeGroupNodeStatus that was added to indicate the failures to the user.
		// This allows the LVMCluster to get deleted and not stuck/wait forever as LVMCluster looks for the LVMVolumeGroupNodeStatus before deleting.
		if statuserr := r.removeVolumeGroupStatus(ctx, volumeGroup.Name); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
			return statuserr
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
		r.Log.Info("failed to find volume group in lvmd deviceclasses list", "VGName", volumeGroup.Name)
		if statuserr := r.removeVolumeGroupStatus(ctx, volumeGroup.Name); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
			return statuserr
		}
		return nil
	}

	// Check if volume group exists
	vg, err := GetVolumeGroup(r.executor, volumeGroup.Name)
	if err != nil {
		if err != ErrVolumeGroupNotFound {
			return fmt.Errorf("failed to get volume group. %q, %v", volumeGroup.Name, err)
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
			err := DeleteLV(r.executor, thinPoolName, volumeGroup.Name)
			if err != nil {
				if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, fmt.Sprintf("failed to delete thin pool %s in volume group %s: %v", thinPoolName, volumeGroup.Name, err.Error())); statuserr != nil {
					r.Log.Error(statuserr, "failed to update status", "name", volumeGroup.Name)
				}
				return fmt.Errorf("failed to delete thin pool %q in volume group %q. %v", thinPoolName, volumeGroup.Name, err)
			}
			r.Log.Info("thin pool deleted in the volume group.", "VGName", volumeGroup.Name, "ThinPool", thinPoolName)
		} else {
			r.Log.Info("thin pool not found in the volume group.", "VGName", volumeGroup.Name, "ThinPool", thinPoolName)
		}
	}

	err = vg.Delete(r.executor)
	if err != nil {
		if statuserr := r.setVolumeGroupFailedStatus(ctx, volumeGroup.Name, fmt.Sprintf("failed to delete volume group %s: %v", volumeGroup.Name, err.Error())); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "name", volumeGroup.Name)
		}
		return fmt.Errorf("failed to delete volume group. %q, %v", volumeGroup.Name, err)
	}

	// Remove this vg from the lvmdconf file
	lvmdConfig.DeviceClasses = append(lvmdConfig.DeviceClasses[:index], lvmdConfig.DeviceClasses[index+1:]...)

	r.Log.Info("updating lvmd config")
	if len(lvmdConfig.DeviceClasses) > 0 {
		err = saveLVMDConfig(lvmdConfig)
		if err != nil {
			r.Log.Error(err, "failed to update lvmd.conf file", "VGName", volumeGroup.Name)
			return err
		}
	} else {
		err = deleteLVMDConfig()
		if err != nil {
			r.Log.Error(err, "failed to delete lvmd.conf file", "VGName", volumeGroup.Name)
			return err
		}
	}

	if statuserr := r.removeVolumeGroupStatus(ctx, volumeGroup.Name); statuserr != nil {
		r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
		return statuserr
	}

	return nil
}

func (r *VGReconciler) setupRAIDThinPool(vg *lvmv1alpha1.LVMVolumeGroup) error {
	resp, err := GetLVSOutput(r.executor, vg.Name)
	if err != nil {
		return fmt.Errorf("failed to list logical volumes in the volume vg %q. %v", vg.Name, err)
	}

	for _, report := range resp.Report {
		for _, lv := range report.Lv {
			if lv.Name == vg.Name {
				return fmt.Errorf("failed to create raid-enabled thinpool thin pool %q. Logical volume with same name already exists, and extension is not possible with RAID configurations", lv.Name)
			}
		}
	}

	args := []string{
		"--type", string(vg.Spec.RAIDConfig.Type),
		"--mirrors", fmt.Sprintf("%d", vg.Spec.RAIDConfig.Mirrors),
		"-l", fmt.Sprintf("%d%%FREE", vg.Spec.ThinPoolConfig.SizePercent),
		"-n", vg.Spec.ThinPoolConfig.Name,
		vg.Name,
	}

	if vg.Spec.RAIDConfig.Stripes > 0 {
		args = append(args, "--stripes", fmt.Sprintf("%d", vg.Spec.RAIDConfig.Stripes))
	}

	if !vg.Spec.RAIDConfig.Sync {
		args = append(args, "--nosync")
		r.Log.Info("raid config without initial sync, potentially dangerous!")
	}

	r.Log.Info("creating RAID array")
	res, err := r.executor.ExecuteCommandWithOutputAsHost(lvCreateCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to create raid array %q in the volume group %q using command '%s': %v",
			vg.Spec.ThinPoolConfig.Name, vg.Name, fmt.Sprintf("%s %s", lvCreateCmd, strings.Join(args, " ")), err)
	}
	r.Log.Info("RAID array was created", "result", res)

	args = []string{
		"--type", "thin-pool",
		"--chunksize", DefaultChunkSize,
		"-Z", "y",
		"-y",
		fmt.Sprintf("%s/%s", vg.Name, vg.Spec.ThinPoolConfig.Name),
	}
	r.Log.Info("Converting RAID array into Thin Pool")
	res, err = r.executor.ExecuteCommandWithOutputAsHost(lvConvertCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to convert raid pool to thin-pool %q in the volume group %q using command '%s': %v",
			vg.Spec.ThinPoolConfig.Name, vg.Name, fmt.Sprintf("%s %s", lvConvertCmd, strings.Join(args, " ")), err)
	}
	r.Log.Info("Thin pool conversion completed", "result", res)

	return nil
}

func (r *VGReconciler) setupThinPool(vgName string, config *lvmv1alpha1.ThinPoolConfig) error {
	resp, err := GetLVSOutput(r.executor, vgName)
	if err != nil {
		return fmt.Errorf("failed to list logical volumes in the volume group %q. %v", vgName, err)
	}

	for _, report := range resp.Report {
		for _, lv := range report.Lv {
			if lv.Name == config.Name {
				if strings.Contains(lv.LvAttr, "t") {
					r.Log.Info("lvm thinpool already exists", "VGName", vgName, "ThinPool", config.Name)
					err = r.extendThinPool(vgName, lv.LvSize, config)
					if err != nil {
						r.Log.Error(err, "failed to extend the lvm thinpool", "VGName", vgName, "ThinPool", config.Name)
					}
					return err
				}

				return fmt.Errorf("failed to create thin pool %q. Logical volume with same name already exists", config.Name)
			}
		}
	}

	args := []string{"-l", fmt.Sprintf("%d%%FREE", config.SizePercent), "-c", DefaultChunkSize, "-Z", "y", "-T", fmt.Sprintf("%s/%s", vgName, config.Name)}

	_, err = r.executor.ExecuteCommandWithOutputAsHost(lvCreateCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to create thin pool %q in the volume group %q using command '%s': %v", config.Name, vgName, fmt.Sprintf("%s %s", lvCreateCmd, strings.Join(args, " ")), err)
	}

	return nil
}

func (r *VGReconciler) extendThinPool(vgName string, lvSize string, config *lvmv1alpha1.ThinPoolConfig) error {

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

	r.Log.Info("extending lvm thinpool ", "VGName", vgName, "ThinPool", config.Name)

	args := []string{"-l", fmt.Sprintf("%d%%Vg", config.SizePercent), fmt.Sprintf("%s/%s", vgName, config.Name)}

	_, err = r.executor.ExecuteCommandWithOutputAsHost(lvExtendCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to extend thin pool %q in the volume group %q using command '%s': %v", config.Name, vgName, fmt.Sprintf("%s %s", lvExtendCmd, strings.Join(args, " ")), err)
	}

	r.Log.Info("successfully extended the thin pool in the volume group ", "thinpool", config.Name, "vgName", vgName)

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
		return nil, err
	} else {
		lvmdconfig := &lvmdCMD.Config{}
		err = yaml.Unmarshal(cfgBytes, lvmdconfig)
		if err != nil {
			return nil, err
		}
		return lvmdconfig, nil
	}
}

func saveLVMDConfig(lvmdConfig *lvmdCMD.Config) error {
	out, err := yaml.Marshal(lvmdConfig)
	if err == nil {
		err = os.WriteFile(controllers.LvmdConfigFile, out, 0600)
	}
	return err
}

func deleteLVMDConfig() error {
	err := os.Remove(controllers.LvmdConfigFile)
	return err
}
