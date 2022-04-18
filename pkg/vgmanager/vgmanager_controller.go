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
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"github.com/red-hat-storage/lvm-operator/controllers"
	"github.com/red-hat-storage/lvm-operator/pkg/internal"
	"github.com/topolvm/topolvm/lvmd"
	lvmdCMD "github.com/topolvm/topolvm/pkg/lvmd/cmd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	ControllerName   = "vg-manager"
	DefaultChunkSize = "512"
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
	r.Log.Info("reconciling", "lvmvolumegroup", req)

	// Check if this lvmvolumegroup needs to be processed on this node
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
	res, err := r.reconcile(ctx, req, volumeGroup)
	if err != nil {
		r.Log.Error(err, "reconcile error", "lvmvolumegroup", req.Name)
	}
	r.Log.Info("reconcile complete", "result", res)
	return res, err

}

var reconcileInterval = time.Minute * 1
var reconcileAgain ctrl.Result = ctrl.Result{Requeue: true, RequeueAfter: reconcileInterval}

//TODO: Refactor this function to move the ctrl result to a single place

func (r *VGReconciler) reconcile(ctx context.Context, req ctrl.Request, volumeGroup *lvmv1alpha1.LVMVolumeGroup) (ctrl.Result, error) {

	// The LVMVolumeGroup resource was deleted
	if !volumeGroup.DeletionTimestamp.IsZero() {
		err := r.processDelete(ctx, volumeGroup)
		return ctrl.Result{}, err
	}

	//Get the block devices that can be used for this volumegroup
	matchingDevices, delayedDevices, err := r.getMatchingDevicesForVG(volumeGroup)
	if err != nil {
		// Failed to get devices for this vg. Reconcile again.
		r.Log.Error(err, "failed to get block devices for volumegroup", "VGName", volumeGroup.Name)
		return reconcileAgain, err
	}

	// Read the lvmd config file
	lvmdConfig, err := loadLVMDConfig()
	if err != nil {
		// Failed to read lvmdconfig file. Reconcile again
		r.Log.Error(err, "failed to read the lvmd config file")
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

	// To avoid having to iterate through device classes multiple times, map from name to config index
	deviceClassMap := make(map[string]int)
	for i, deviceClass := range lvmdConfig.DeviceClasses {
		deviceClassMap[deviceClass.Name] = i
	}

	status := &lvmv1alpha1.VGStatus{
		Name:   req.Name,
		Status: lvmv1alpha1.VGStatusReady,
		Reason: "",
	}
	_, found := deviceClassMap[volumeGroup.Name]
	if found {
		volGrpHostInfo, err := GetVolumeGroup(r.executor, volumeGroup.Name)
		if err != nil {
			r.Log.Error(err, "failed to get volume group from the host", "VGName", volumeGroup.Name)
		} else {
			status.Devices = volGrpHostInfo.PVs
		}
	}

	if len(matchingDevices) == 0 {
		r.Log.Info("no matching devices for volume group", "VGName", volumeGroup.Name)
		if len(delayedDevices) > 0 {
			return reconcileAgain, nil
		}

		if found {
			// Update the status again just to be safe.
			if statuserr := r.updateStatus(ctx); statuserr != nil {
				r.Log.Error(statuserr, "failed to update status", "name", volumeGroup.Name)
				return reconcileAgain, nil
			}
		}
		return ctrl.Result{}, nil
	}

	// create/extend VG and update lvmd config
	err = r.addDevicesToVG(volumeGroup.Name, matchingDevices)
	if err != nil {
		r.Log.Error(err, "failed to create/extend volume group", "VGName", volumeGroup.Name)

		if statuserr := r.updateStatus(ctx); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
		}
		return reconcileAgain, err
	}

	if !found {
		dc := &lvmd.DeviceClass{
			Name:           volumeGroup.Name,
			VolumeGroup:    volumeGroup.Name,
			Default:        true,
			ThinPoolConfig: &lvmd.ThinPoolConfig{},
		}

		if volumeGroup.Spec.ThinPoolConfig != nil {
			dc.Type = lvmd.TypeThin
			dc.ThinPoolConfig.Name = volumeGroup.Spec.ThinPoolConfig.Name
			dc.ThinPoolConfig.OverprovisionRatio = float64(volumeGroup.Spec.ThinPoolConfig.OverprovisionRatio)
		}

		lvmdConfig.DeviceClasses = append(lvmdConfig.DeviceClasses, dc)
	}

	// Create thin pool
	err = r.addThinPoolToVG(volumeGroup.Name, volumeGroup.Spec.ThinPoolConfig)
	if err != nil {
		r.Log.Error(err, "failed to create thin pool", "VGName", "ThinPool", volumeGroup.Name, volumeGroup.Spec.ThinPoolConfig.Name)
	}

	// apply and save lvmconfig
	// pass config to configChannel only if config has changed
	if !cmp.Equal(existingLvmdConfig, lvmdConfig) {
		err := saveLVMDConfig(lvmdConfig)
		if err != nil {
			r.Log.Error(err, "failed to update lvmd.conf file", "VGName", volumeGroup.Name)
			return reconcileAgain, err
		}
		r.Log.Info("updated lvmd config", "VGName", volumeGroup.Name)
	}

	if err == nil {
		if statuserr := r.updateStatus(ctx); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
			return reconcileAgain, nil
		}
	} else {
		r.Log.Error(err, "failed to get volume group from the host", "name", volumeGroup.Name)
	}

	// requeue faster if some devices are too recently observed to consume
	requeueAfter := time.Minute * 1
	if len(delayedDevices) > 0 {
		requeueAfter = time.Second * 30
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *VGReconciler) addThinPoolToVG(vgName string, config *lvmv1alpha1.ThinPoolConfig) error {
	resp, err := GetLVSOutput(r.executor, vgName)
	if err != nil {
		return fmt.Errorf("failed to list logical volumes in the volume group %q. %v", vgName, err)
	}

	for _, report := range resp.Report {
		for _, lv := range report.Lv {
			if lv.Name == config.Name {
				if strings.Contains(lv.LvAttr, "t") {
					r.Log.Info("lvm thinpool already exists", "VGName", vgName, "ThinPool", config.Name)
					return nil
				}

				return fmt.Errorf("failed to create thin pool %q. Logical volume with same name already exists", config.Name)
			}
		}
	}

	args := []string{"-l", fmt.Sprintf("%d%%FREE", config.SizePercent), "-c", DefaultChunkSize, "-T", fmt.Sprintf("%s/%s", vgName, config.Name)}

	_, err = r.executor.ExecuteCommandWithOutputAsHost(lvCreateCmd, args...)
	if err != nil {
		return fmt.Errorf("failed to create thin pool %q in the volume group %q. %v", config.Name, vgName, err)
	}

	return nil
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
				return fmt.Errorf("failed to delete thin pool %q in volume group %q. %v", thinPoolName, volumeGroup.Name, err)
			}
			r.Log.Info("thin pool deleted in the volume group.", "VGName", volumeGroup.Name, "ThinPool", thinPoolName)
		} else {
			r.Log.Info("thin pool not found in the volume group.", "VGName", volumeGroup.Name, "ThinPool", thinPoolName)
		}
	}

	err = vg.Delete(r.executor)
	if err != nil {
		return fmt.Errorf("failed to delete volume group. %q, %v", volumeGroup.Name, err)
	}

	// Remove this vg from the lvmdconf file
	lvmdConfig.DeviceClasses = append(lvmdConfig.DeviceClasses[:index], lvmdConfig.DeviceClasses[index+1:]...)
	//r.Log.Info("After delete: ", "deviceclasses", lvmdConfig.DeviceClasses)

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

	if statuserr := r.updateStatus(ctx); statuserr != nil {
		r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
		return statuserr
	}
	return err
}

func (r *VGReconciler) addDevicesToVG(vgName string, devices []internal.BlockDevice) error {
	if len(devices) < 1 {
		return fmt.Errorf("can't create vg %q with 0 devices", vgName)
	}

	// check if volume group is already present
	vgs, err := ListVolumeGroups(r.executor)
	if err != nil {
		return fmt.Errorf("failed to list volume groups. %v", err)
	}

	vgFound := false
	for _, vg := range vgs {
		if vg.Name == vgName {
			vgFound = true
		}
	}

	args := []string{vgName}
	for _, device := range devices {
		args = append(args, fmt.Sprintf("/dev/%s", device.KName))
	}

	var cmd string
	if vgFound {
		r.Log.Info("extending an existing volume group", "VGName", vgName)
		cmd = "/usr/sbin/vgextend"
	} else {
		r.Log.Info("creating a new volume group", "VGName", vgName)
		cmd = "/usr/sbin/vgcreate"
	}

	_, err = r.executor.ExecuteCommandWithOutputAsHost(cmd, args...)
	if err != nil {
		return fmt.Errorf("failed to create or extend volume group %q. %v", vgName, err)
	}

	return nil
}

// filterMatchingDevices returns unmatched and matched blockdevices
// TODO: Implement this
func filterMatchingDevices(blockDevices []internal.BlockDevice, volumeGroup *lvmv1alpha1.LVMVolumeGroup) ([]internal.BlockDevice, []internal.BlockDevice, error) {
	// currently just match all devices
	return []internal.BlockDevice{}, blockDevices, nil
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

func (r *VGReconciler) getMatchingDevicesForVG(volumeGroup *lvmv1alpha1.LVMVolumeGroup) (matching []internal.BlockDevice, delayed []internal.BlockDevice, err error) {
	// The LVMVolumeGroup was created/modified
	r.Log.Info("getting block devices for volumegroup", "VGName", volumeGroup.Name)

	//  list block devices
	blockDevices, err := internal.ListBlockDevices(r.executor)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list block devices: %v", err)
	}

	// filter out block devices
	remainingValidDevices, delayedDevices, err := r.filterAvailableDevices(blockDevices)
	if err != nil {
		_ = err
	}

	var matchingDevices []internal.BlockDevice
	_, matchingDevices, err = filterMatchingDevices(remainingValidDevices, volumeGroup)
	if err != nil {
		r.Log.Error(err, "could not filter matching devices", "VGName", volumeGroup.Name)
		return nil, nil, err
	}

	return matchingDevices, delayedDevices, nil
}

func (r *VGReconciler) generateVolumeGroupNodeStatus() (*lvmv1alpha1.LVMVolumeGroupNodeStatus, error) {

	vgs, err := ListVolumeGroups(r.executor)
	if err != nil {
		return nil, fmt.Errorf("failed to list volume groups. %v", err)
	}

	//lvmvgstatus := vgNodeStatus.Spec.LVMVGStatus
	var statusarr []lvmv1alpha1.VGStatus

	for _, vg := range vgs {
		newStatus := &lvmv1alpha1.VGStatus{
			Name:    vg.Name,
			Reason:  "test",
			Devices: vg.PVs,
		}
		statusarr = append(statusarr, *newStatus)
	}

	vgNodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.NodeName,
			Namespace: r.Namespace,
		},
		Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
			LVMVGStatus: statusarr,
		},
	}

	return vgNodeStatus, nil
}

func (r *VGReconciler) updateStatus(ctx context.Context) error {

	vgNodeStatus, err := r.generateVolumeGroupNodeStatus()

	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.NodeName,
			Namespace: r.Namespace,
		},
	}

	if err != nil {
		r.Log.Error(err, "failed to generate nodeStatus")
		return err
	}

	result, err := ctrl.CreateOrUpdate(ctx, r.Client, nodeStatus, func() error {
		nodeStatus.Spec.LVMVGStatus = vgNodeStatus.Spec.LVMVGStatus
		return nil
	})

	if err != nil {
		r.Log.Error(err, "failed to create or update lvmvolumegroupnodestatus", "name", vgNodeStatus.Name)
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("lvmvolumegroupnodestatus modified", "operation", result, "name", vgNodeStatus.Name)
	} else {
		r.Log.Info("lvmvolumegroupnodestatus unchanged")
	}
	return err
}
