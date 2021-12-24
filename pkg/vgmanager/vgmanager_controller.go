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
	"k8s.io/apimachinery/pkg/runtime"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		For(&lvmv1alpha1.LVMCluster{}).
		Complete(r)
}

type VGReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	// map from KNAME of device to time when the device was first observed since the process started
	deviceAgeMap *ageMap
}

func (r *VGReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log = log.FromContext(ctx).WithName(ControllerName)
	r.Log.Info("reconciling", "lvmcluster", req)
	res, err := r.reconcile(ctx, req)
	if err != nil {
		r.Log.Error(err, "reconcile error")
	}
	r.Log.Info("reconcile complete", "result", res)
	return res, err

}
func (r *VGReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lvmCluster := &lvmv1alpha1.LVMCluster{}
	err := r.Client.Get(ctx, req.NamespacedName, lvmCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.Log.Info("listing block devices")
	//  list block devices
	blockDevices, badRows, err := internal.ListBlockDevices()
	if err != nil {
		msg := fmt.Sprintf("failed to list block devices: %v", err)
		r.Log.Error(err, msg, "lsblk.BadRows", badRows)
		return ctrl.Result{}, err
	} else if len(badRows) > 0 {
		r.Log.Error(err, "could not parse all the lsblk rows", "lsblk.BadRows", badRows)
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
		return ctrl.Result{}, err
	} else {
		err = yaml.Unmarshal(cfgBytes, &lvmdConfig)
		if err != nil {
			return ctrl.Result{}, err
		}
		existingLvmdConfig = lvmdConfig
	}

	// avoid having to iterate through device classes multiple times, map from name to config index
	deviceClassMap := make(map[string]int)
	for i, deviceClass := range lvmdConfig.DeviceClasses {
		deviceClassMap[deviceClass.Name] = i
	}

	// filter out block devices
	remainingValidDevices, delayedDevices, err := r.filterAvailableDevices(blockDevices)
	if err != nil {
		_ = err
	}

	for _, deviceClass := range lvmCluster.Spec.DeviceClasses {
		// ignore deviceClasses whose LabelSelector doesn't match this node
		// NodeSelectorTerms.MatchExpressions are ORed
		node := &corev1.Node{}
		selectsNode, err := NodeSelectorMatchesNodeLabels(node, deviceClass.NodeSelector)
		if err != nil {
			r.Log.Error(err, "failed to match nodeSelector to node labels")
			continue
		}
		if !(selectsNode && ToleratesTaints(deviceClass.Tolerations, node.Spec.Taints)) {
			continue
		}
		_, found := deviceClassMap[deviceClass.Name]
		if !found {
			lvmdConfig.DeviceClasses = append(lvmdConfig.DeviceClasses, &lvmd.DeviceClass{
				Name:        deviceClass.Name,
				VolumeGroup: deviceClass.Name,
				Default:     true,
			})
		}
		var matchingDevices []internal.BlockDevice
		remainingValidDevices, matchingDevices, err = filterMatchingDevices(remainingValidDevices, deviceClass)
		if err != nil {
			r.Log.Error(err, "could not filterMatchingDevices")
			continue
		}
		if len(matchingDevices) > 0 {
			// create/update VG and update lvmd config
			err = r.addMatchingDevicesToVG(matchingDevices, deviceClass.Name)
			if err != nil {
				r.Log.Error(err, "could not prepare volume group", "name", deviceClass.Name)
				continue
			}
		}
	}

	// apply and save lvmconfig
	// pass config to configChannel only if config has changed
	if !cmp.Equal(existingLvmdConfig, lvmdConfig) {
		r.Log.Info("updating lvmd config")
		out, err := yaml.Marshal(lvmdConfig)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = os.WriteFile(controllers.LvmdConfigFile, out, 0600)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// requeue faster if some devices are too recently observed to consume
	requeueAfter := time.Minute * 2
	if len(delayedDevices) > 0 {
		requeueAfter = time.Second * 30
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// filterMatchingDevices returns unmatched and matched blockdevices
func filterMatchingDevices(blockDevices []internal.BlockDevice, lvmCluster lvmv1alpha1.DeviceClass) ([]internal.BlockDevice, []internal.BlockDevice, error) {
	// currently just match all devices
	return []internal.BlockDevice{}, blockDevices, nil
}

func NodeSelectorMatchesNodeLabels(node *corev1.Node, nodeSelector *corev1.NodeSelector) (bool, error) {
	if nodeSelector == nil {
		return true, nil
	}
	if node == nil {
		return false, fmt.Errorf("the node var is nil")
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
