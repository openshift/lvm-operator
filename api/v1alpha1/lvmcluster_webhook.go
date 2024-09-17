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
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openshift/lvm-operator/internal/cluster"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ThinPoolConfigMaxRecommendedSizePercent is the maximum recommended size percent for the thin pool.
const ThinPoolConfigMaxRecommendedSizePercent = 90

// log is for logging in this package.
var lvmclusterlog = logf.Log.WithName("lvmcluster-webhook")

type lvmClusterValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &lvmClusterValidator{}

var (
	ErrDeviceClassNotFound                                   = errors.New("DeviceClass not found in the LVMCluster")
	ErrThinPoolConfigNotSet                                  = errors.New("ThinPoolConfig is not set for the DeviceClass")
	ErrInvalidNamespace                                      = errors.New("invalid namespace was supplied")
	ErrAtLeastOneDeviceClassRequired                         = errors.New("at least one deviceClass is required")
	ErrOnlyOneDefaultDeviceClassAllowed                      = errors.New("only one default deviceClass is allowed")
	ErrPathsOrOptionalPathsMandatoryWithNonNilDeviceSelector = errors.New("either paths or optionalPaths must be specified when DeviceSelector is specified")
	ErrEmptyPathsWithMultipleDeviceClasses                   = errors.New("path list should not be empty when there are multiple deviceClasses")
	ErrDuplicateLVMCluster                                   = errors.New("duplicate LVMClusters are not allowed, remove the old LVMCluster or work with the existing instance")
	ErrThinPoolConfigCannotBeChanged                         = errors.New("ThinPoolConfig can not be changed")
	ErrDevicePathsCannotBeAddedInUpdate                      = errors.New("device paths can not be added after a device class has been initialized")
	ErrForceWipeOptionCannotBeChanged                        = errors.New("ForceWipeDevicesAndDestroyAllData can not be changed")
)

//+kubebuilder:webhook:path=/validate-lvm-topolvm-io-v1alpha1-lvmcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=lvm.topolvm.io,resources=lvmclusters,verbs=create;update,versions=v1alpha1,name=vlvmcluster.kb.io,admissionReviewVersions=v1

func (l *LVMCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(l).
		WithValidator(&lvmClusterValidator{Client: mgr.GetClient()}).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *lvmClusterValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	l := obj.(*LVMCluster)

	warnings := admission.Warnings{}
	lvmclusterlog.Info("validate create", "name", l.Name)

	if namespace, err := cluster.GetOperatorNamespace(); err != nil {
		return warnings, fmt.Errorf("could not verify namespace of lvmcluster: %w", err)
	} else if namespace != l.GetNamespace() {
		return warnings, fmt.Errorf(
			"creating LVMCluster is only supported within namespace %q: %w",
			namespace, ErrInvalidNamespace,
		)
	}

	existing := &LVMClusterList{}
	if err := v.List(ctx, existing, &client.ListOptions{Limit: 1, Namespace: l.GetNamespace()}); err != nil {
		return warnings, fmt.Errorf("could not verify that LVMCluster was not already created %w", err)
	} else if len(existing.Items) > 0 {
		return warnings, fmt.Errorf("LVMCluster exists at %q: %w",
			client.ObjectKeyFromObject(&existing.Items[0]), ErrDuplicateLVMCluster)
	}

	deviceClassWarnings, err := v.verifyDeviceClass(l)
	warnings = append(warnings, deviceClassWarnings...)
	if err != nil {
		return warnings, err
	}

	err = v.verifyPathsAreNotEmpty(l)
	if err != nil {
		return warnings, err
	}

	err = v.verifyAbsolutePath(l)
	if err != nil {
		return warnings, err
	}

	err = v.verifyNoDeviceOverlap(l)
	if err != nil {
		return warnings, err
	}

	err = v.verifyFstype(l)
	if err != nil {
		return warnings, err
	}

	return warnings, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *lvmClusterValidator) ValidateUpdate(_ context.Context, old, new runtime.Object) (admission.Warnings, error) {
	l := new.(*LVMCluster)

	lvmclusterlog.Info("validate update", "name", l.Name)
	warnings := admission.Warnings{}

	deviceClassWarnings, err := v.verifyDeviceClass(l)
	warnings = append(warnings, deviceClassWarnings...)
	if err != nil {
		return warnings, err
	}

	err = v.verifyPathsAreNotEmpty(l)
	if err != nil {
		return warnings, err
	}

	err = v.verifyAbsolutePath(l)
	if err != nil {
		return warnings, err
	}

	err = v.verifyNoDeviceOverlap(l)
	if err != nil {
		return warnings, err
	}

	err = v.verifyFstype(l)
	if err != nil {
		return warnings, err
	}

	oldLVMCluster, ok := old.(*LVMCluster)
	if !ok {
		return warnings, fmt.Errorf("failed to parse LVMCluster")
	}

	// Validate all the old device classes still exist
	err = validateDeviceClassesStillExist(oldLVMCluster.Spec.Storage.DeviceClasses, l.Spec.Storage.DeviceClasses)
	if err != nil {
		return warnings, fmt.Errorf("invalid: device classes were deleted from the LVMCluster: %w", err)
	}

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		var newThinPoolConfig, oldThinPoolConfig *ThinPoolConfig
		var newDevices, newOptionalDevices, oldDevices, oldOptionalDevices []string
		var oldForceWipeOption, newForceWipeOption *bool

		newThinPoolConfig = deviceClass.ThinPoolConfig
		oldThinPoolConfig, err = v.getThinPoolsConfigOfDeviceClass(oldLVMCluster, deviceClass.Name)

		if (newThinPoolConfig != nil && oldThinPoolConfig == nil && !errors.Is(err, ErrDeviceClassNotFound)) ||
			(newThinPoolConfig == nil && oldThinPoolConfig != nil) {
			return warnings, fmt.Errorf("ThinPoolConfig can not be changed")
		}

		if newThinPoolConfig != nil && oldThinPoolConfig != nil {
			if newThinPoolConfig.Name != oldThinPoolConfig.Name {
				return warnings, fmt.Errorf("ThinPoolConfig.Name is invalid: %w", ErrThinPoolConfigCannotBeChanged)
			} else if newThinPoolConfig.SizePercent != oldThinPoolConfig.SizePercent {
				return warnings, fmt.Errorf("ThinPoolConfig.SizePercent is invalid: %w", ErrThinPoolConfigCannotBeChanged)
			} else if newThinPoolConfig.OverprovisionRatio != oldThinPoolConfig.OverprovisionRatio {
				return warnings, fmt.Errorf("ThinPoolConfig.OverprovisionRatio is invalid: %w", ErrThinPoolConfigCannotBeChanged)
			}
		}

		if deviceClass.DeviceSelector != nil {
			newDevices = deviceClass.DeviceSelector.Paths
			newOptionalDevices = deviceClass.DeviceSelector.OptionalPaths
			newForceWipeOption = deviceClass.DeviceSelector.ForceWipeDevicesAndDestroyAllData
		}

		oldDevices, oldOptionalDevices, oldForceWipeOption, err = v.getPathsOfDeviceClass(oldLVMCluster, deviceClass.Name)

		// Is this a new device class?
		if err == ErrDeviceClassNotFound {
			continue
		}

		// Make sure ForceWipeDevicesAndDestroyAllData was not changed
		if (oldForceWipeOption == nil && newForceWipeOption != nil) ||
			(oldForceWipeOption != nil && newForceWipeOption == nil) ||
			(oldForceWipeOption != nil && newForceWipeOption != nil &&
				*oldForceWipeOption != *newForceWipeOption) {
			return warnings, ErrForceWipeOptionCannotBeChanged
		}

		// Make sure a device path list was not added
		if len(oldDevices) == 0 && len(newDevices) > 0 {
			return warnings, ErrDevicePathsCannotBeAddedInUpdate
		}

		// Make sure an optionalPaths list was not added
		if len(oldOptionalDevices) == 0 && len(newOptionalDevices) > 0 {
			return warnings, ErrDevicePathsCannotBeAddedInUpdate
		}

		// Validate all the old paths still exist
		err := validateDevicePathsStillExist(oldDevices, newDevices)
		if err != nil {
			return warnings, fmt.Errorf("invalid: required device paths were deleted from the LVMCluster: %w", err)
		}

		// Validate all the old optional paths still exist
		err = validateDevicePathsStillExist(oldOptionalDevices, newOptionalDevices)
		if err != nil {
			return warnings, fmt.Errorf("invalid: optional device paths were deleted from the LVMCluster: %w", err)
		}
	}

	return warnings, nil
}

func validateDeviceClassesStillExist(old, new []DeviceClass) error {
	deviceClassMap := make(map[string]bool)

	for _, deviceClass := range old {
		deviceClassMap[deviceClass.Name] = true
	}

	for _, deviceClass := range new {
		delete(deviceClassMap, deviceClass.Name)
	}

	// if any old device class is removed now
	if len(deviceClassMap) != 0 {
		return fmt.Errorf("device classes can not be removed from the LVMCluster once added oldDeviceClasses:%v, newDeviceClasses:%v", old, new)
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
func (v *lvmClusterValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	l := obj.(*LVMCluster)

	lvmclusterlog.Info("validate delete", "name", l.Name)

	return []string{}, nil
}

func (v *lvmClusterValidator) verifyDeviceClass(l *LVMCluster) (admission.Warnings, error) {
	deviceClasses := l.Spec.Storage.DeviceClasses
	if len(deviceClasses) < 1 {
		return nil, ErrAtLeastOneDeviceClassRequired
	}
	countDefault := 0
	warnings := admission.Warnings{}
	for _, deviceClass := range deviceClasses {
		if deviceClass.Default {
			countDefault++
		}
		if tpConfig := deviceClass.ThinPoolConfig; tpConfig != nil {
			tpWarnings, err := v.verifyThinPoolConfig(tpConfig)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, tpWarnings...)
		}
	}
	if countDefault > 1 {
		return nil, fmt.Errorf("%w. Currently, there are %d default deviceClasses", ErrOnlyOneDefaultDeviceClassAllowed, countDefault)
	}

	if countDefault == 0 {
		warnings = append(warnings, "no default deviceClass was specified, it will be mandatory to specify the generated storage class in any PVC explicitly")
	}

	return warnings, nil
}

func (v *lvmClusterValidator) verifyPathsAreNotEmpty(l *LVMCluster) error {

	var deviceClassesWithoutPaths []string
	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.DeviceSelector != nil {
			if len(deviceClass.DeviceSelector.Paths) == 0 && len(deviceClass.DeviceSelector.OptionalPaths) == 0 {
				return ErrPathsOrOptionalPathsMandatoryWithNonNilDeviceSelector
			}
		} else {
			deviceClassesWithoutPaths = append(deviceClassesWithoutPaths, deviceClass.Name)
		}
	}
	if len(l.Spec.Storage.DeviceClasses) > 1 && len(deviceClassesWithoutPaths) > 0 {
		return fmt.Errorf("%w. Please specify device path(s) under deviceSelector.paths for %s deviceClass(es)", ErrEmptyPathsWithMultipleDeviceClasses, strings.Join(deviceClassesWithoutPaths, `,`))
	}

	return nil
}

func (v *lvmClusterValidator) verifyThinPoolConfig(config *ThinPoolConfig) (admission.Warnings, error) {
	if config.SizePercent <= ThinPoolConfigMaxRecommendedSizePercent {
		return nil, nil
	}
	return admission.Warnings{fmt.Sprintf(
		"ThinPoolConfig.SizePercent for %[1]s is greater than %[2]d%%, "+
			"this may lead to issues once the thin pool metadata that is created by default is nearing full capacity, "+
			"as it will be impossible to extent the metadata pool size. "+
			"You can ignore this warning if "+
			"a) you are certain that you do not need to extend the metadata pool in the future or "+
			"b) you set it above %[2]d%% but below 100%% because the buffer is sufficiently big with a smaller reserved percentage",
		config.Name, ThinPoolConfigMaxRecommendedSizePercent,
	)}, nil
}

func (v *lvmClusterValidator) verifyAbsolutePath(l *LVMCluster) error {
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

func (v *lvmClusterValidator) verifyNoDeviceOverlap(l *LVMCluster) error {

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

func (v *lvmClusterValidator) getPathsOfDeviceClass(l *LVMCluster, deviceClassName string) (required []string, optional []string, forceWipe *bool, err error) {
	required, optional, err = []string{}, []string{}, nil
	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.Name == deviceClassName {
			if deviceClass.DeviceSelector != nil {
				required = deviceClass.DeviceSelector.Paths
				optional = deviceClass.DeviceSelector.OptionalPaths
				forceWipe = deviceClass.DeviceSelector.ForceWipeDevicesAndDestroyAllData
			}

			return
		}
	}

	err = ErrDeviceClassNotFound
	return
}

func (v *lvmClusterValidator) getThinPoolsConfigOfDeviceClass(l *LVMCluster, deviceClassName string) (*ThinPoolConfig, error) {

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

func (v *lvmClusterValidator) verifyFstype(l *LVMCluster) error {
	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.FilesystemType != FilesystemTypeExt4 && deviceClass.FilesystemType != FilesystemTypeXFS {
			return fmt.Errorf("fstype '%s' is not a supported filesystem type", deviceClass.FilesystemType)
		}
	}

	return nil
}
