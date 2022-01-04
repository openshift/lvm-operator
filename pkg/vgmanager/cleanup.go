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
	"errors"
	"fmt"
	"os"

	"github.com/red-hat-storage/lvm-operator/controllers"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	lvmCMD "github.com/topolvm/topolvm/pkg/lvmd/cmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// vgCleanupAll removes all volume groups existing in lvmd config file and after
// removing every vg it also removes lvmd config file
func vgCleanupAll(r *VGReconciler, ctx context.Context) error {

	// get limited number of logical volume CRs to verify their presence
	lvs := &topolvmv1.LogicalVolumeList{}
	// TODO: perform cleanup based on individual volume groups after comparing
	// lvmd config file with lvmcluster CR
	if err := r.Client.List(ctx, lvs, &client.ListOptions{Limit: int64(1)}); err == nil {
		// do not proceed with cleanup if there are logicalVolumes
		if len(lvs.Items) > 0 {
			return fmt.Errorf("found presence of logicalvolumes and aborting volume group cleanup")
		}
	} else {
		r.Log.Error(err, "failed to list logicalvolumes CRs")
		return err
	}

	// get all the volume groups from lvmd file
	lvmdConfig := &lvmCMD.Config{
		SocketName: controllers.DefaultLVMdSocket,
	}

	cfgBytes, err := os.ReadFile(controllers.LvmdConfigFile)
	if os.IsNotExist(err) {
		r.Log.Info("lvmd config file doesn't exist, no need of cleanup")
		return nil
	} else if err != nil {
		r.Log.Error(err, "failed to read lvmd config file", "file path", controllers.LvmdConfigFile)
		return err
	} else {
		err = yaml.Unmarshal(cfgBytes, &lvmdConfig)
		if err != nil {
			r.Log.Error(err, "failed to unmarshal yaml contents of lvmd config data")
			return err
		}
	}

	// remove volume group sequentially
	var done int
	isStarted := false
	for done = 0; done < len(lvmdConfig.DeviceClasses); done++ {
		deviceClass := lvmdConfig.DeviceClasses[done]
		if err = RemoveVolumeGroup(deviceClass.Name); err != nil {
			if errors.Is(err, ErrNotFound) {
				// TODO: simplify the logic, multiple controllers are mutating same
				// LVMCluster CR and as a result during the reconciliation cleanup is
				// happening but failing to remove finalizer. So, if we try to remove
				// a VG and it isn't found then that was probably deleted in previous
				// reconile and so mark that the cleanup process was initiated
				isStarted = true
			}
			r.Log.Error(err, "failed to remove volume group", "deviceClass", deviceClass.Name)
			break
		}
		if !isStarted {
			isStarted = true
		}
	}

	if !isStarted && len(lvmdConfig.DeviceClasses) > 0 {
		// not able to start cleanup, no need to overwrite config file, just return error
		return fmt.Errorf("failed to initiate cleanup of Volume Groups")
	} else if done == len(lvmdConfig.DeviceClasses)-1 || len(lvmdConfig.DeviceClasses) == 0 {
		// every volume group is deleted and config file can be deleted
		if err = os.Remove(controllers.LvmdConfigFile); err != nil {
			return fmt.Errorf("failed to remove LVMd config file")
		}
	} else if done != 0 {
		// some vgs are pending for deletion, overwrite config file
		lvmdConfig.DeviceClasses = lvmdConfig.DeviceClasses[done:]
		config, err := yaml.Marshal(lvmdConfig)
		if err != nil {
			r.Log.Error(err, "failed to marshal yaml contents of lvmd config data")
			return err
		}
		if err = os.WriteFile(controllers.LvmdConfigFile, config, 0600); err != nil {
			return fmt.Errorf("failed to overwrite LVMd config file")
		}
		pending := make([]string, 0, len(lvmdConfig.DeviceClasses))
		for _, dc := range lvmdConfig.DeviceClasses {
			pending = append(pending, dc.Name)
		}
		r.Log.Info("device classes are pending for deletion", "deviceClasses", pending)
	}

	return nil
}
