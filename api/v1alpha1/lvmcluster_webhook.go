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
	"reflect"
	"strings"

	"github.com/openshift/lvm-operator/v4/internal/cluster"

	corev1 "k8s.io/api/core/v1"
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
	ErrNodeSelectorNotSet                                    = errors.New("NodeSelector is not set for the DeviceClass")
	ErrInvalidNamespace                                      = errors.New("invalid namespace was supplied")
	ErrOnlyOneDefaultDeviceClassAllowed                      = errors.New("only one default deviceClass is allowed")
	ErrPathsOrOptionalPathsMandatoryWithNonNilDeviceSelector = errors.New("either paths or optionalPaths must be specified when DeviceSelector is specified")
	ErrEmptyPathsWithMultipleDeviceClasses                   = errors.New("path list should not be empty when there are multiple deviceClasses")
	ErrDuplicateLVMCluster                                   = errors.New("duplicate LVMClusters are not allowed, remove the old LVMCluster or work with the existing instance")
	ErrThinPoolConfigCannotBeChanged                         = errors.New("ThinPoolConfig can not be changed")
	ErrThinPoolMetadataSizeCanOnlyBeIncreased                = errors.New("thin pool metadata size can only be increased")
	ErrNodeSelectorCannotBeChanged                           = errors.New("NodeSelector can not be changed")
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

	pathWarnings, err := v.verifyPathsAreNotEmpty(l)
	warnings = append(warnings, pathWarnings...)
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

	err = v.verifyChunkSize(l)
	if err != nil {
		return warnings, err
	}

	metadataWarnings, err := v.verifyMetadataSize(l)
	if err != nil {
		return warnings, err
	}
	warnings = append(warnings, metadataWarnings...)
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

	pathWarnings, err := v.verifyPathsAreNotEmpty(l)
	warnings = append(warnings, pathWarnings...)
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
		var newDevices, newOptionalDevices, oldDevices, oldOptionalDevices []DevicePath
		var oldForceWipeOption, newForceWipeOption *bool

		newThinPoolConfig = deviceClass.ThinPoolConfig
		oldThinPoolConfig, err = v.getThinPoolsConfigOfDeviceClass(oldLVMCluster, deviceClass.Name)

		if (newThinPoolConfig != nil && oldThinPoolConfig == nil && !errors.Is(err, ErrDeviceClassNotFound)) ||
			(newThinPoolConfig == nil && oldThinPoolConfig != nil) {
			return warnings, ErrThinPoolConfigCannotBeChanged
		}

		if newThinPoolConfig != nil && oldThinPoolConfig != nil {
			if newThinPoolConfig.Name != oldThinPoolConfig.Name {
				return warnings, fmt.Errorf("ThinPoolConfig.Name is invalid: %w", ErrThinPoolConfigCannotBeChanged)
			} else if newThinPoolConfig.SizePercent != oldThinPoolConfig.SizePercent {
				return warnings, fmt.Errorf("ThinPoolConfig.SizePercent is invalid: %w", ErrThinPoolConfigCannotBeChanged)
			} else if newThinPoolConfig.ChunkSizeCalculationPolicy != oldThinPoolConfig.ChunkSizeCalculationPolicy {
				return warnings, fmt.Errorf("ThinPoolConfig.ChunkSizeCalculationPolicy is invalid: %w", ErrThinPoolConfigCannotBeChanged)
			} else if !reflect.DeepEqual(newThinPoolConfig.ChunkSize, oldThinPoolConfig.ChunkSize) {
				return warnings, fmt.Errorf("ThinPoolConfig.ChunkSize is invalid: %w", ErrThinPoolConfigCannotBeChanged)
			}

			if newThinPoolConfig.MetadataSizeCalculationPolicy == MetadataSizePolicyStatic {
				if newThinPoolConfig.MetadataSize == nil {
					warnings = append(warnings, "thin pool metadata size is unset. LVMS operator will automatically set it to 1Gb and grow metadata size if needed")
					newThinPoolConfig.MetadataSize = &ThinPoolMetadataSizeDefault
				}
				if oldThinPoolConfig.MetadataSizeCalculationPolicy == MetadataSizePolicyStatic {
					if oldThinPoolConfig.MetadataSize == nil {
						oldThinPoolConfig.MetadataSize = &ThinPoolMetadataSizeDefault
					}
					if newThinPoolConfig.MetadataSize.Value() < oldThinPoolConfig.MetadataSize.Value() {
						return warnings, fmt.Errorf("ThinPoolConfig.MetadataSize is invalid: %w", ErrThinPoolMetadataSizeCanOnlyBeIncreased)
					}
				}
			}
		}

		newNodeSelector := deviceClass.NodeSelector
		oldNodeSelector, err := v.getNodeSelectorOfDeviceClass(oldLVMCluster, deviceClass.Name)
		if (newNodeSelector != nil && oldNodeSelector == nil && !errors.Is(err, ErrDeviceClassNotFound)) ||
			(newNodeSelector == nil && oldNodeSelector != nil) ||
			(newNodeSelector != nil && oldNodeSelector != nil && !reflect.DeepEqual(newNodeSelector, oldNodeSelector)) {
			return warnings, ErrNodeSelectorCannotBeChanged
		}

		if deviceClass.DeviceSelector != nil {
			newDevices = deviceClass.DeviceSelector.Paths
			newOptionalDevices = deviceClass.DeviceSelector.OptionalPaths
			newForceWipeOption = deviceClass.DeviceSelector.ForceWipeDevicesAndDestroyAllData
		}

		oldDevices, oldOptionalDevices, oldForceWipeOption, err = v.getPathsOfDeviceClass(oldLVMCluster, deviceClass.Name)

		// Is this a new device class?
		if errors.Is(err, ErrDeviceClassNotFound) {
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

		if err := validateDevicePathsStillExist(oldDevices, newDevices); err != nil {
			return warnings, fmt.Errorf("invalid: required device paths were deleted from the LVMCluster: %w", err)
		}

		if err := validateDevicePathsStillExist(oldOptionalDevices, newOptionalDevices); err != nil {
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

func validateDevicePathsStillExist(old, new []DevicePath) error {
	deviceMap := make(map[DevicePath]struct{})

	for _, device := range old {
		deviceMap[device] = struct{}{}
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
		warnings = append(warnings, "no default deviceClass was specified, it will be mandatory to specify the generated storage class in any PVC explicitly or you will have to declare another default StorageClass")
	}

	return warnings, nil
}

func (v *lvmClusterValidator) verifyPathsAreNotEmpty(l *LVMCluster) (admission.Warnings, error) {

	var deviceClassesWithoutPaths []string
	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.DeviceSelector != nil {
			if len(deviceClass.DeviceSelector.Paths) == 0 && len(deviceClass.DeviceSelector.OptionalPaths) == 0 {
				return nil, ErrPathsOrOptionalPathsMandatoryWithNonNilDeviceSelector
			}
		} else {
			deviceClassesWithoutPaths = append(deviceClassesWithoutPaths, deviceClass.Name)
		}
	}
	if len(l.Spec.Storage.DeviceClasses) > 1 && len(deviceClassesWithoutPaths) > 0 {
		return nil, fmt.Errorf("%w. Please specify device path(s) under deviceSelector.paths for %s deviceClass(es)", ErrEmptyPathsWithMultipleDeviceClasses, strings.Join(deviceClassesWithoutPaths, `,`))
	} else if len(l.Spec.Storage.DeviceClasses) == 1 && len(deviceClassesWithoutPaths) == 1 {
		return admission.Warnings{fmt.Sprintf("no device path(s) under deviceSelector.paths was specified for the %s deviceClass, LVMS will actively monitor and dynamically utilize any supported unused devices. This is not recommended for production environments. Please refer to the limitations outlined in the product documentation for further details.", deviceClassesWithoutPaths[0])}, nil
	}

	return nil, nil
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
				if !strings.HasPrefix(path.Unresolved(), "/dev/") {
					return fmt.Errorf("path %s must be an absolute path to the device", path.Unresolved())
				}
			}

			for _, path := range deviceClass.DeviceSelector.OptionalPaths {
				if !strings.HasPrefix(path.Unresolved(), "/dev/") {
					return fmt.Errorf("optional path %s must be an absolute path to the device", path.Unresolved())
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
	devices := make(map[string]map[DevicePath]string)

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {

		if deviceClass.DeviceSelector == nil {
			continue
		}

		nodeSelector := deviceClass.NodeSelector.String()

		// Required paths
		for _, path := range deviceClass.DeviceSelector.Paths {
			if val, ok := devices[nodeSelector][path]; ok {
				if val != deviceClass.Name {
					return fmt.Errorf("error: device path %s overlaps in two different deviceClasss %s and %s", path, val, deviceClass.Name)
				}
				return fmt.Errorf("error: device path %s is specified at multiple places in deviceClass %s", path, val)
			}

			if devices[nodeSelector] == nil {
				devices[nodeSelector] = make(map[DevicePath]string)
			}

			devices[nodeSelector][path] = deviceClass.Name
		}

		// Optional paths
		for _, path := range deviceClass.DeviceSelector.OptionalPaths {
			if val, ok := devices[nodeSelector][path]; ok {
				if val != deviceClass.Name {
					return fmt.Errorf("error: optional device path %s overlaps in two different deviceClasss %s and %s", path, val, deviceClass.Name)
				}
				return fmt.Errorf("error: optional device path %s is specified at multiple places in deviceClass %s", path, val)
			}

			if devices[nodeSelector] == nil {
				devices[nodeSelector] = make(map[DevicePath]string)
			}

			devices[nodeSelector][path] = deviceClass.Name
		}
	}

	return nil
}

func (v *lvmClusterValidator) getPathsOfDeviceClass(l *LVMCluster, deviceClassName string) (required []DevicePath, optional []DevicePath, forceWipe *bool, err error) {
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

func (v *lvmClusterValidator) getNodeSelectorOfDeviceClass(l *LVMCluster, deviceClassName string) (*corev1.NodeSelector, error) {

	for _, deviceClass := range l.Spec.Storage.DeviceClasses {
		if deviceClass.Name == deviceClassName {
			if deviceClass.NodeSelector != nil {
				return deviceClass.NodeSelector, nil
			}
			return nil, ErrNodeSelectorNotSet
		}
	}

	return nil, ErrDeviceClassNotFound
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

func (v *lvmClusterValidator) verifyChunkSize(l *LVMCluster) error {
	for _, dc := range l.Spec.Storage.DeviceClasses {
		if dc.ThinPoolConfig == nil {
			continue
		}
		if dc.ThinPoolConfig.ChunkSizeCalculationPolicy == ChunkSizeCalculationPolicyHost && dc.ThinPoolConfig.ChunkSize != nil {
			return fmt.Errorf("chunk size can not be set when chunk size calculation policy is set to Host")
		}

		if dc.ThinPoolConfig.ChunkSize != nil {
			if dc.ThinPoolConfig.ChunkSize.Cmp(ChunkSizeMinimum) < 0 {
				return fmt.Errorf("chunk size must be greater than or equal to %s", ChunkSizeMinimum.String())
			}
			if dc.ThinPoolConfig.ChunkSize.Cmp(ChunkSizeMaximum) > 0 {
				return fmt.Errorf("chunk size must be less than or equal to %s", ChunkSizeMaximum.String())
			}
		}
	}

	return nil
}

func (v *lvmClusterValidator) verifyMetadataSize(l *LVMCluster) ([]string, error) {
	warnings := make([]string, 0)
	for _, dc := range l.Spec.Storage.DeviceClasses {
		if dc.ThinPoolConfig == nil {
			continue
		}
		if dc.ThinPoolConfig.MetadataSizeCalculationPolicy == MetadataSizePolicyHost && dc.ThinPoolConfig.MetadataSize != nil {
			return warnings, fmt.Errorf("metadata size can not be set when metadata size calculation policy is set to Host")
		}
		if dc.ThinPoolConfig.MetadataSizeCalculationPolicy == MetadataSizePolicyStatic && dc.ThinPoolConfig.MetadataSize == nil {
			warnings = append(warnings, "metadata size in unset. LVMS will set it to 1Gi by default")
			dc.ThinPoolConfig.MetadataSize = &ThinPoolMetadataSizeDefault
		}
		if dc.ThinPoolConfig.MetadataSize != nil {
			if dc.ThinPoolConfig.MetadataSize.Cmp(ThinPoolMetadataSizeMinimum) < 0 {
				return warnings, fmt.Errorf("metadata size must be greater than or equal to %s", ThinPoolMetadataSizeMinimum.String())
			}
			if dc.ThinPoolConfig.MetadataSize.Cmp(ThinPoolMetadataSizeMaximum) > 0 {
				return warnings, fmt.Errorf("metadata size must be less than or equal to %s", ThinPoolMetadataSizeMaximum.String())
			}
		}
	}
	return warnings, nil
}
