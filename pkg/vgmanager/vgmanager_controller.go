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
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"github.com/red-hat-storage/lvm-operator/controllers"
	"github.com/red-hat-storage/lvm-operator/pkg/internal"
	"github.com/topolvm/topolvm/lvmd"
	lvmdCMD "github.com/topolvm/topolvm/pkg/lvmd/cmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	ControllerName = "vg-manager"
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
	r.executor = &internal.CommandExecutor{}
	res, err := r.reconcile(ctx, req)
	if err != nil {
		r.Log.Error(err, "reconcile error")
	}
	r.Log.Info("reconcile complete", "result", res)
	return res, err

}

var reconcileInterval = time.Minute * 1
var reconcileAgain ctrl.Result = ctrl.Result{Requeue: true, RequeueAfter: reconcileInterval}

//TODO: Refactor this function to move the ctrl result to a single place

func (r *VGReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	volumeGroup := &lvmv1alpha1.LVMVolumeGroup{}
	err := r.Client.Get(ctx, req.NamespacedName, volumeGroup)
	if err != nil {
		r.Log.Error(err, "failed to get LVMVolumeGroup", "VGName", req.Name)
		return ctrl.Result{}, err
	}

	//TODO: actually check the node against the nodeSelector.
	node := &corev1.Node{}
	nodeMatches, err := NodeSelectorMatchesNodeLabels(node, volumeGroup.Spec.NodeSelector)
	if err != nil {
		r.Log.Error(err, "failed to match nodeSelector to node labels", "VGName", volumeGroup.Name)
		return reconcileAgain, err
	}

	if !nodeMatches {
		//Nothing to be done on this node for the VG.
		r.Log.Info("node does not match selector", "VGName", volumeGroup.Name)
		return ctrl.Result{}, nil
	}

	r.Log.Info("listing block devices", "VGName", volumeGroup.Name)

	//  list block devices
	blockDevices, err := internal.ListBlockDevices(r.executor)
	if err != nil {
		return reconcileAgain, fmt.Errorf("failed to list block devices: %v", err)
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
		return reconcileAgain, err
	}

	status := &lvmv1alpha1.VGStatus{
		Name:   req.Name,
		Status: lvmv1alpha1.VGStatusReady,
		Reason: "",
	}

	existingLvmdConfig := &lvmdCMD.Config{}

	// load lvmd config
	lvmdConfig := &lvmdCMD.Config{
		SocketName: controllers.DefaultLVMdSocket,
	}

	cfgBytes, err := os.ReadFile(controllers.LvmdConfigFile)
	if os.IsNotExist(err) {
		r.Log.Info("lvmd config file doesn't exist, will create")
	} else if err != nil {
		return reconcileAgain, err
	} else {
		err = yaml.Unmarshal(cfgBytes, &lvmdConfig)
		if err != nil {
			return reconcileAgain, err
		}
		existingLvmdConfig = lvmdConfig
	}

	// avoid having to iterate through device classes multiple times, map from name to config index
	deviceClassMap := make(map[string]int)
	for i, deviceClass := range lvmdConfig.DeviceClasses {
		deviceClassMap[deviceClass.Name] = i
	}

	_, found := deviceClassMap[volumeGroup.Name]
	if found {
		volGrpHostInfo, err := GetVolumeGroup(r.executor, volumeGroup.Name)
		if err != nil {
			r.Log.Error(err, "failed to get volume group", "name", volumeGroup.Name)
		} else {
			status.Devices = volGrpHostInfo.PVs
		}
	}

	if len(matchingDevices) == 0 {
		r.Log.Info("no matching devices", "VGName", volumeGroup.Name)
		if len(delayedDevices) > 0 {
			return reconcileAgain, nil
		} else {
			if found {
				// Update the status again just to be safe.
				if statuserr := r.updateStatus(ctx, status, volumeGroup); statuserr != nil {
					r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
					return reconcileAgain, nil
				}

			}
			return ctrl.Result{}, nil
		}
	}

	// create/extend VG and update lvmd config
	err = r.addDevicesToVG(volumeGroup.Name, matchingDevices)
	if err != nil {
		r.Log.Error(err, "failed to create/extend volume group", "VGName", volumeGroup.Name)

		if !found {
			status.Status = lvmv1alpha1.VGStatusFailed
			status.Reason = "VGCreationFailed"
		} else {
			status.Status = lvmv1alpha1.VGStatusDegraded
			status.Reason = "VGExtendFailed"
		}
		if statuserr := r.updateStatus(ctx, status, volumeGroup); statuserr != nil {
			r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
		}
		return reconcileAgain, err
	}

	if !found {
		lvmdConfig.DeviceClasses = append(lvmdConfig.DeviceClasses, &lvmd.DeviceClass{
			Name:        volumeGroup.Name,
			VolumeGroup: volumeGroup.Name,
			Default:     true,
		})
	}

	// apply and save lvmconfig
	// pass config to configChannel only if config has changed
	if !cmp.Equal(existingLvmdConfig, lvmdConfig) {
		r.Log.Info("updating lvmd config")
		out, err := yaml.Marshal(lvmdConfig)
		if err == nil {
			err = os.WriteFile(controllers.LvmdConfigFile, out, 0600)
		}

		if err != nil {
			r.Log.Error(err, "failed to update lvmd.conf file", "VGName", volumeGroup.Name)
			return reconcileAgain, err
		}
	}

	volGrpHostInfo, err := GetVolumeGroup(r.executor, volumeGroup.Name)
	if err == nil {
		status.Devices = volGrpHostInfo.PVs
	} else {
		r.Log.Error(err, "failed to get volume group", "name", volumeGroup.Name)
	}

	if statuserr := r.updateStatus(ctx, status, volumeGroup); statuserr != nil {
		r.Log.Error(statuserr, "failed to update status", "VGName", volumeGroup.Name)
	}

	// requeue faster if some devices are too recently observed to consume
	requeueAfter := time.Minute * 1
	if len(delayedDevices) > 0 {
		requeueAfter = time.Second * 30
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
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
		r.Log.Info("extending an existing volume group", "Name", vgName)
		cmd = "/usr/sbin/vgextend"
	} else {
		r.Log.Info("creating a new volume group", "Name", vgName)
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

func ToleratesTaints(tolerations []corev1.Toleration, taints []corev1.Taint) bool {
	for _, t := range taints {
		taint := t
		toleratesTaint := corev1helper.TolerationsTolerateTaint(tolerations, &taint)
		if !toleratesTaint {
			return false
		}
	}
	return true
}

func setStatus(status *lvmv1alpha1.VGStatus, instance *lvmv1alpha1.LVMVolumeGroupNodeStatus) {
	found := false

	vgStatuses := instance.Spec.LVMVGStatus
	for i, vgStatus := range vgStatuses {
		if vgStatus.Name == status.Name {
			found = true
			vgStatuses[i] = *status
			break
		}
	}

	if !found {
		newStatus := &lvmv1alpha1.VGStatus{
			Name:    status.Name,
			Status:  status.Status,
			Reason:  status.Reason,
			Devices: status.Devices,
		}
		vgStatuses = append(vgStatuses, *newStatus)
		instance.Spec.LVMVGStatus = vgStatuses
	}
}

func (r *VGReconciler) updateStatus(ctx context.Context, status *lvmv1alpha1.VGStatus, instance *lvmv1alpha1.LVMVolumeGroup) error {

	vgNodeStatus := r.getNewNodeStatus(status)

	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.NodeName,
			Namespace: r.Namespace,
		},
	}

	result, err := ctrl.CreateOrUpdate(ctx, r.Client, nodeStatus, func() error {
		if nodeStatus.CreationTimestamp.IsZero() {
			vgNodeStatus.DeepCopyInto(nodeStatus)
			return nil
		}
		setStatus(status, nodeStatus)
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

func (r *VGReconciler) getNewNodeStatus(status *lvmv1alpha1.VGStatus) *lvmv1alpha1.LVMVolumeGroupNodeStatus {

	vgNodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.NodeName,
			Namespace: r.Namespace,
		},
		Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{},
	}
	setStatus(status, vgNodeStatus)
	return vgNodeStatus
}
