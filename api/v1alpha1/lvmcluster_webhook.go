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

	err = l.verifyFstype()
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

	err = l.verifyFstype()
	if err != nil {
		return err
	}

	oldLVMCluster, ok := old.(*LVMCluster)
	if !ok {
		return fmt.Errorf("Failed to parse LVMCluster.")
	}

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		var newThinPoolConfig, oldThinPoolConfig *ThinPoolConfig
		var newDevices, newOptionalDevices, oldDevices, oldOptionalDevices []string

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
			newOptionalDevices = deviceClass.DeviceSelector.OptionalPaths
		}

		oldDevices, oldOptionalDevices, err = oldLVMCluster.getPathsOfDeviceClass(deviceClass.Name)

		// Is this a new device class?
		if err == ErrDeviceClassNotFound {
			continue
		}

		// Make sure a device path list was not added
		if len(oldDevices) == 0 && len(newDevices) > 0 {
			return fmt.Errorf("invalid: device paths can not be added after a device class has been initialized")
		}

		// Make sure an optionalPaths list was not added
		if len(oldOptionalDevices) == 0 && len(newOptionalDevices) > 0 {
			return fmt.Errorf("invalid: optional device paths can not be added after a device class has been initialized")
		}

		// Validate all the old paths still exist
		err := validateDevicePathsStillExist(oldDevices, newDevices)
		if err != nil {
			return fmt.Errorf("invalid: required device paths were deleted from the LVMCluster: %v", err)
		}

		// Validate all the old optional paths still exist
		err = validateDevicePathsStillExist(oldOptionalDevices, newOptionalDevices)
		if err != nil {
			return fmt.Errorf("invalid: optional device paths were deleted from the LVMCluster: %v", err)
		}
	}

	return nil
}

func validateDevicePathsStillExist(old, new []string) error {
	deviceMap := make(map[string]bool)

	for _, device := range old {
		deviceMap[device] = true
	}

	for _, device := range new {
		delete(deviceMap, device)
	}

	// if any old device is removed now
	if len(deviceMap) != 0 {
		return fmt.Errorf("devices can not be removed from the LVMCluster once added oldDevices:%s, newDevices:%s", old, new)
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

	var deviceClassesWithoutPaths []string
	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.DeviceSelector != nil {
			if len(deviceClass.DeviceSelector.Paths) == 0 && len(deviceClass.DeviceSelector.OptionalPaths) == 0 {
				return fmt.Errorf("either paths or optionalPaths must be specified when DeviceSelector is specified")
			}
		} else {
			deviceClassesWithoutPaths = append(deviceClassesWithoutPaths, deviceClass.Name)
		}
	}
	if len(l.Spec.Storage.DeviceClasses) > 1 && len(deviceClassesWithoutPaths) > 0 {
		return fmt.Errorf("path list should not be empty when there are multiple deviceClasses. Please specify device path(s) under deviceSelector.paths for %s deviceClass(es)", strings.Join(deviceClassesWithoutPaths, `,`))
	}

	return nil
}

func (l *LVMCluster) verifyAbsolutePath() error {

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.DeviceSelector != nil {
			for _, path := range deviceClass.DeviceSelector.Paths {
				if !strings.HasPrefix(path, "/dev/") {
					return fmt.Errorf("path %s must be an absolute path to the device", path)
				}
			}

			for _, path := range deviceClass.DeviceSelector.OptionalPaths {
				if !strings.HasPrefix(path, "/dev/") {
					return fmt.Errorf("optional path %s must be an absolute path to the device", path)
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

			// Required paths
			for _, path := range deviceClass.DeviceSelector.Paths {
				if val, ok := devices[nodeSelector][path]; ok {
					var err error
					if val != deviceClass.Name {
						err = fmt.Errorf("error: device path %s overlaps in two different deviceClasss %s and %s", path, val, deviceClass.Name)
					} else {
						err = fmt.Errorf("error: device path %s is specified at multiple places in deviceClass %s", path, val)
					}
					return err
				}

				if devices[nodeSelector] == nil {
					devices[nodeSelector] = make(map[string]string)
				}

				devices[nodeSelector][path] = deviceClass.Name
			}

			// Optional paths
			for _, path := range deviceClass.DeviceSelector.OptionalPaths {
				if val, ok := devices[nodeSelector][path]; ok {
					var err error
					if val != deviceClass.Name {
						err = fmt.Errorf("error: optional device path %s overlaps in two different deviceClasss %s and %s", path, val, deviceClass.Name)
					} else {
						err = fmt.Errorf("error: optional device path %s is specified at multiple places in deviceClass %s", path, val)
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

func (l *LVMCluster) getPathsOfDeviceClass(deviceClassName string) (required []string, optional []string, err error) {
	required, optional, err = []string{}, []string{}, nil
	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.Name == deviceClassName {
			if deviceClass.DeviceSelector != nil {
				required = deviceClass.DeviceSelector.Paths
				optional = deviceClass.DeviceSelector.OptionalPaths
			}

			return
		}
	}

	err = ErrDeviceClassNotFound
	return
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

func (l *LVMCluster) verifyFstype() error {
	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.FilesystemType != FilesystemTypeExt4 && deviceClass.FilesystemType != FilesystemTypeXFS {
			return fmt.Errorf("fstype '%s' is not a supported filesystem type", deviceClass.FilesystemType)
		}
	}

	return nil
}
