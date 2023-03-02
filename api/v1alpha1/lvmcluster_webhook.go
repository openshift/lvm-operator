/*
Copyright 2022 Red Hat Openshift Data Foundation.

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

package v1alpha1

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var lvmclusterlog = logf.Log.WithName("lvmcluster-webhook")

var _ webhook.Validator = &LVMCluster{}

var (
	ErrDeviceClassNotFound  = fmt.Errorf("DeviceClass not found in the LVMCluster")
	ErrThinPoolConfigNotSet = fmt.Errorf("ThinPoolConfig is not set for the DeviceClass")
)

//+kubebuilder:webhook:path=/validate-lvm-topolvm-io-v1alpha1-lvmcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=lvm.topolvm.io,resources=lvmclusters,verbs=create;update,versions=v1alpha1,name=vlvmcluster.kb.io,admissionReviewVersions=v1

func (l *LVMCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(l).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (l *LVMCluster) ValidateCreate() error {
	lvmclusterlog.Info("validate create", "name", l.Name)

	err := l.verifySingleDefaultDeviceClass()
	if err != nil {
		return err
	}

	err = l.verifyPathsAreNotEmpty()
	if err != nil {
		return err
	}

	err = l.verifyAbsolutePath()
	if err != nil {
		return err
	}

	err = l.verifyNoDeviceOverlap()
	if err != nil {
		return err
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (l *LVMCluster) ValidateUpdate(old runtime.Object) error {
	lvmclusterlog.Info("validate update", "name", l.Name)

	err := l.verifySingleDefaultDeviceClass()
	if err != nil {
		return err
	}

	err = l.verifyPathsAreNotEmpty()
	if err != nil {
		return err
	}

	err = l.verifyAbsolutePath()
	if err != nil {
		return err
	}

	err = l.verifyNoDeviceOverlap()
	if err != nil {
		return err
	}

	oldLVMCluster, ok := old.(*LVMCluster)
	if !ok {
		return fmt.Errorf("Failed to parse LVMCluster.")
	}

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		var newThinPoolConfig, oldThinPoolConfig *ThinPoolConfig
		var newDevices, oldDevices []string

		newThinPoolConfig = deviceClass.ThinPoolConfig
		oldThinPoolConfig, err = oldLVMCluster.getThinPoolsConfigOfDeviceClass(deviceClass.Name)

		if (newThinPoolConfig != nil && oldThinPoolConfig == nil && err != ErrDeviceClassNotFound) ||
			(newThinPoolConfig == nil && oldThinPoolConfig != nil) {
			return fmt.Errorf("ThinPoolConfig can not be changed")
		}

		if newThinPoolConfig != nil && oldThinPoolConfig != nil {
			if newThinPoolConfig.Name != oldThinPoolConfig.Name {
				return fmt.Errorf("ThinPoolConfig.Name can not be changed")
			} else if newThinPoolConfig.SizePercent != oldThinPoolConfig.SizePercent {
				return fmt.Errorf("ThinPoolConfig.SizePercent can not be changed")
			} else if newThinPoolConfig.OverprovisionRatio != oldThinPoolConfig.OverprovisionRatio {
				return fmt.Errorf("ThinPoolConfig.OverprovisionRatio can not be changed")
			}
		}

		if deviceClass.DeviceSelector != nil {
			newDevices = deviceClass.DeviceSelector.Paths
		}

		oldDevices, err = oldLVMCluster.getPathsOfDeviceClass(deviceClass.Name)

		// if devices are removed now
		if len(oldDevices) > len(newDevices) {
			return fmt.Errorf("Invalid:devices can not be removed from the LVMCluster once added.")
		}

		// if devices are added now
		if len(oldDevices) == 0 && len(newDevices) > 0 && err != ErrDeviceClassNotFound {
			return fmt.Errorf("Invalid:devices can not be added in the LVMCluster once created without devices.")
		}

		deviceMap := make(map[string]bool)

		for _, device := range oldDevices {
			deviceMap[device] = true
		}

		for _, device := range newDevices {
			delete(deviceMap, device)
		}

		// if any old device is removed now
		if len(deviceMap) != 0 {
			return fmt.Errorf("Invalid:some of devices are deleted from the LVMCluster. "+
				"Device can not be removed from the LVMCluster once added. "+
				"oldDevices:%s, newDevices:%s", oldDevices, newDevices)
		}
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (l *LVMCluster) ValidateDelete() error {
	lvmclusterlog.Info("validate delete", "name", l.Name)

	return nil
}

func (l *LVMCluster) verifySingleDefaultDeviceClass() error {
	deviceClasses := l.Spec.Storage.DeviceClasses
	if len(deviceClasses) == 1 {
		return nil
	} else if len(deviceClasses) < 1 {
		return fmt.Errorf("at least one deviceClass is required")
	}
	countDefault := 0
	for _, deviceClass := range deviceClasses {
		if deviceClass.Default {
			countDefault++
		}
	}
	if countDefault < 1 {
		return fmt.Errorf("one default deviceClass is required. Please specify default=true for the default deviceClass")
	} else if countDefault > 1 {
		return fmt.Errorf("only one default deviceClass is allowed. Currently, there are %d default deviceClasses", countDefault)
	}

	return nil
}

func (l *LVMCluster) verifyPathsAreNotEmpty() error {

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.DeviceSelector != nil {
			if len(deviceClass.DeviceSelector.Paths) == 0 {
				return fmt.Errorf("path list should not be empty when DeviceSelector is specified")
			}
		} else {
			if len(l.Spec.Storage.DeviceClasses) > 1 {
				return fmt.Errorf("path list should not be empty when there are multiple deviceClasses. Please specify device path(s) under deviceSelector.paths for %s deviceClass", deviceClass.Name)
			}
		}
	}

	return nil
}

func (l *LVMCluster) verifyAbsolutePath() error {

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.DeviceSelector != nil {
			for _, path := range deviceClass.DeviceSelector.Paths {
				if !strings.HasPrefix(path, "/dev/") {
					return fmt.Errorf("Given path %s is not an absolute path. "+
						"Please provide the absolute path to the device", path)
				}
			}
		}
	}

	return nil
}

func (l *LVMCluster) verifyNoDeviceOverlap() error {

	// make sure no device overlap with another VGs
	// use map to find the duplicate entries for paths
	/*
		{
		  "nodeSelector1": {
		        "/dev/sda": "vg1",
		        "/dev/sdb": "vg1"
		    },
		    "nodeSelector2": {
		        "/dev/sda": "vg1",
		        "/dev/sdb": "vg1"
		    }
		}
	*/
	devices := make(map[string]map[string]string)

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.DeviceSelector != nil {
			nodeSelector := deviceClass.NodeSelector.String()
			for _, path := range deviceClass.DeviceSelector.Paths {
				if val, ok := devices[nodeSelector][path]; ok {
					var err error
					if val != deviceClass.Name {
						err = fmt.Errorf("Error: device path %s overlaps in two different deviceClasss %s and %s", path, val, deviceClass.Name)
					} else {
						err = fmt.Errorf("Error: device path %s is specified at multiple places in deviceClass %s", path, val)
					}
					return err
				}

				if devices[nodeSelector] == nil {
					devices[nodeSelector] = make(map[string]string)
				}

				devices[nodeSelector][path] = deviceClass.Name
			}
		}
	}

	return nil
}

func (l *LVMCluster) getPathsOfDeviceClass(deviceClassName string) ([]string, error) {

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.Name == deviceClassName {
			if deviceClass.DeviceSelector != nil {
				return deviceClass.DeviceSelector.Paths, nil
			}
			return []string{}, nil
		}
	}

	return []string{}, ErrDeviceClassNotFound
}

func (l *LVMCluster) getThinPoolsConfigOfDeviceClass(deviceClassName string) (*ThinPoolConfig, error) {

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.Name == deviceClassName {
			if deviceClass.ThinPoolConfig != nil {
				return deviceClass.ThinPoolConfig, nil
			}
			return nil, ErrThinPoolConfigNotSet
		}
	}

	return nil, ErrDeviceClassNotFound
}
