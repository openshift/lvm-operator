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

package resource

import (
	"context"
	"fmt"
	"reflect"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	scName = "topolvm-storageclass"
)

func TopoLVMStorageClass() Manager {
	return &topolvmStorageClass{}
}

type topolvmStorageClass struct{}

// topolvmStorageClass unit satisfies resourceManager interface
var _ Manager = topolvmStorageClass{}

func (s topolvmStorageClass) GetName() string {
	return scName
}

//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;create;delete;watch;list;update;patch

func (s topolvmStorageClass) EnsureCreated(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	desiredStorageClasses := s.getTopolvmStorageClasses(r, ctx, cluster)

	for _, desired := range desiredStorageClasses {
		existing := &storagev1.StorageClass{}
		err := r.Get(ctx, types.NamespacedName{Name: desired.Name}, existing)

		if err != nil {
			if errors.IsNotFound(err) {
				labels.SetManagedLabels(r.Scheme(), desired, cluster)
				s.applyAdditionalLabels(desired, cluster, desired.Name)
				if err := r.Create(ctx, desired); err != nil {
					return fmt.Errorf("%s failed to create StorageClass %s: %w", s.GetName(), desired.Name, err)
				}
				logger.V(2).Info("StorageClass created", "name", desired.Name)
				continue
			}
			return fmt.Errorf("%s failed to get StorageClass %s: %w", s.GetName(), desired.Name, err)
		}

		if s.needsRecreation(existing, desired) {
			if err := r.Delete(ctx, existing); err != nil {
				return fmt.Errorf("%s failed to delete StorageClass %s: %w", s.GetName(), desired.Name, err)
			}
			logger.V(2).Info("StorageClass deleted for recreation", "name", desired.Name)
			continue
		}

		labels.SetManagedLabels(r.Scheme(), existing, cluster)
		s.applyAdditionalLabels(existing, cluster, desired.Name)
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("%s failed to update StorageClass %s: %w", s.GetName(), existing.Name, err)
		}
	}

	return nil
}

func (s topolvmStorageClass) EnsureDeleted(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	// construct name of storage class based on CR spec deviceClass field and
	// delete the corresponding storage class
	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		scName := GetStorageClassName(deviceClass.Name)

		sc := &storagev1.StorageClass{}
		if err := r.Get(ctx, types.NamespacedName{Name: scName}, sc); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		if !sc.GetDeletionTimestamp().IsZero() {
			// Already being deleted - watch event will trigger next reconcile
			logger.V(2).Info("StorageClass deletion in progress", "name", scName)
			continue
		}

		if err := r.Delete(ctx, sc); err != nil {
			return fmt.Errorf("failed to delete StorageClass %s: %w", scName, err)
		}
		logger.V(2).Info("StorageClass deleted", "name", scName)
	}
	return nil
}

func (s topolvmStorageClass) getTopolvmStorageClasses(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) []*storagev1.StorageClass {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	const defaultSCAnnotation string = "storageclass.kubernetes.io/is-default-class"
	allowVolumeExpansion := true
	defaultStorageClassName := ""
	setDefaultStorageClass := true

	scList := &storagev1.StorageClassList{}
	err := r.List(ctx, scList)

	if err != nil {
		logger.Error(err, "failed to list storage classes. Not setting any storageclass as the default")
		setDefaultStorageClass = false
	} else {
		for _, sc := range scList.Items {
			v := sc.Annotations[defaultSCAnnotation]
			if v == "true" {
				defaultStorageClassName = sc.Name
				break
			}
		}
	}
	var sc []*storagev1.StorageClass
	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		scName := GetStorageClassName(deviceClass.Name)

		volumeBindingMode := storagev1.VolumeBindingWaitForFirstConsumer
		var reclaimPolicy *corev1.PersistentVolumeReclaimPolicy
		parameters := map[string]string{
			constants.DeviceClassKey:    deviceClass.Name,
			"csi.storage.k8s.io/fstype": string(deviceClass.FilesystemType),
		}
		additionalLabels := map[string]string{}

		if opts := deviceClass.StorageClassOptions; opts != nil {
			if opts.VolumeBindingMode != nil {
				volumeBindingMode = *opts.VolumeBindingMode
			}

			if opts.ReclaimPolicy != nil {
				reclaimPolicy = opts.ReclaimPolicy
			}

			lvmsOwnedParams := map[string]bool{
				constants.DeviceClassKey:    true,
				"csi.storage.k8s.io/fstype": true,
			}
			for key, value := range opts.AdditionalParameters {
				if !lvmsOwnedParams[key] {
					parameters[key] = value
				} else {
					logger.V(1).Info("Skipping conflicting additionalParameter",
						"key", key, "deviceClass", deviceClass.Name)
				}
			}

			if opts.AdditionalLabels != nil {
				additionalLabels = opts.AdditionalLabels
			}
		}

		storageClass := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: scName,
				Annotations: map[string]string{
					"description": "Provides RWO and RWOP Filesystem & Block volumes",
				},
			},
			Provisioner:          constants.TopolvmCSIDriverName,
			VolumeBindingMode:    &volumeBindingMode,
			ReclaimPolicy:        reclaimPolicy,
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters:           parameters,
		}

		if len(additionalLabels) > 0 {
			if storageClass.Labels == nil {
				storageClass.Labels = make(map[string]string)
			}
			for key, value := range additionalLabels {
				storageClass.Labels[key] = value
			}
		}

		if deviceClass.Default && setDefaultStorageClass && (defaultStorageClassName == "" || defaultStorageClassName == scName) {
			storageClass.Annotations[defaultSCAnnotation] = "true"
			defaultStorageClassName = scName
		}
		sc = append(sc, storageClass)
	}
	return sc
}

func (s topolvmStorageClass) needsRecreation(existing, desired *storagev1.StorageClass) bool {
	existingRP := corev1.PersistentVolumeReclaimDelete
	desiredRP := corev1.PersistentVolumeReclaimDelete
	if existing.ReclaimPolicy != nil {
		existingRP = *existing.ReclaimPolicy
	}
	if desired.ReclaimPolicy != nil {
		desiredRP = *desired.ReclaimPolicy
	}
	if existingRP != desiredRP {
		return true
	}

	existingVBM := storagev1.VolumeBindingWaitForFirstConsumer
	desiredVBM := storagev1.VolumeBindingWaitForFirstConsumer
	if existing.VolumeBindingMode != nil {
		existingVBM = *existing.VolumeBindingMode
	}
	if desired.VolumeBindingMode != nil {
		desiredVBM = *desired.VolumeBindingMode
	}
	if existingVBM != desiredVBM {
		return true
	}

	existingParams := existing.Parameters
	desiredParams := desired.Parameters
	if existingParams == nil {
		existingParams = map[string]string{}
	}
	if desiredParams == nil {
		desiredParams = map[string]string{}
	}
	return !reflect.DeepEqual(existingParams, desiredParams)
}

func (s topolvmStorageClass) applyAdditionalLabels(sc *storagev1.StorageClass, cluster *lvmv1alpha1.LVMCluster, scName string) {
	var deviceClass *lvmv1alpha1.DeviceClass
	for i, dc := range cluster.Spec.Storage.DeviceClasses {
		if GetStorageClassName(dc.Name) == scName {
			deviceClass = &cluster.Spec.Storage.DeviceClasses[i]
			break
		}
	}

	if deviceClass == nil || deviceClass.StorageClassOptions == nil {
		return
	}

	if sc.Labels == nil {
		sc.Labels = make(map[string]string)
	}

	lvmsManagedLabels := map[string]bool{
		constants.AppKubernetesPartOfLabel:    true,
		constants.AppKubernetesNameLabel:      true,
		constants.AppKubernetesManagedByLabel: true,
		constants.AppKubernetesComponentLabel: true,
	}

	for key, value := range deviceClass.StorageClassOptions.AdditionalLabels {
		if !lvmsManagedLabels[key] {
			sc.Labels[key] = value
		}
	}
}
