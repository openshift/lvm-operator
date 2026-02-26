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
	"maps"
	"sort"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
		sc := &storagev1.StorageClass{}
		sc.Name = desired.Name

		result, err := cutil.CreateOrUpdate(ctx, r, sc, func() error {
			labels.SetManagedLabels(r.Scheme(), sc, cluster)
			applyAdditionalLabels(ctx, sc, cluster, sc.Name)

			// Only set immutable spec fields on creation (ResourceVersion is empty for new objects)
			if sc.ResourceVersion == "" {
				sc.Provisioner = desired.Provisioner
				sc.VolumeBindingMode = desired.VolumeBindingMode
				sc.ReclaimPolicy = desired.ReclaimPolicy
				sc.AllowVolumeExpansion = desired.AllowVolumeExpansion
				sc.Parameters = make(map[string]string, len(desired.Parameters))
				maps.Copy(sc.Parameters, desired.Parameters)
				for k, v := range desired.Annotations {
					if sc.Annotations == nil {
						sc.Annotations = make(map[string]string)
					}
					sc.Annotations[k] = v
				}
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("%s failed to reconcile StorageClass %s: %w", s.GetName(), desired.Name, err)
		}
		if result != cutil.OperationResultNone {
			logger.V(2).Info("StorageClass reconciled", "operation", result, "name", sc.Name)
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
		logger := logger.WithValues("StorageClass", scName)

		sc := &storagev1.StorageClass{}
		if err := r.Get(ctx, types.NamespacedName{Name: scName}, sc); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		if !sc.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the StorageClass %s is still present, waiting for deletion", scName)
		}

		if err := r.Delete(ctx, sc); err != nil {
			return fmt.Errorf("failed to delete StorageClass %s: %w", scName, err)
		}
		logger.Info("initiated StorageClass deletion")
	}
	return nil
}

func (s topolvmStorageClass) getTopolvmStorageClasses(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) []*storagev1.StorageClass {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	const defaultSCAnnotation string = "storageclass.kubernetes.io/is-default-class"
	allowVolumeExpansion := true
	defaultStorageClassName := ""
	setDefaultStorageClass := true

	// Mark the lvms storage class, associated with the default device class, as default if no other default storage class exists on the cluster
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

		// Defaults
		reclaimPolicy := corev1.PersistentVolumeReclaimDelete
		volumeBindingMode := storagev1.VolumeBindingWaitForFirstConsumer

		// Apply StorageClassOptions overrides
		if opts := deviceClass.StorageClassOptions; opts != nil {
			if opts.ReclaimPolicy != nil {
				reclaimPolicy = *opts.ReclaimPolicy
			}
			if opts.VolumeBindingMode != nil {
				volumeBindingMode = *opts.VolumeBindingMode
			}
		}

		parameters := map[string]string{
			constants.DeviceClassKey: deviceClass.Name,
			constants.FsTypeKey:      string(deviceClass.FilesystemType),
		}

		// Merge additional parameters, skipping LVMS-owned keys
		if deviceClass.StorageClassOptions != nil {
			for k, v := range deviceClass.StorageClassOptions.AdditionalParameters {
				if k == constants.DeviceClassKey || k == constants.FsTypeKey {
					continue
				}
				parameters[k] = v
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
			ReclaimPolicy:        &reclaimPolicy,
			VolumeBindingMode:    &volumeBindingMode,
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters:           parameters,
		}
		// reconcile will pick up any existing LVMO storage classes as well
		if deviceClass.Default && setDefaultStorageClass && (defaultStorageClassName == "" || defaultStorageClassName == scName) {
			storageClass.Annotations[defaultSCAnnotation] = "true"
			defaultStorageClassName = scName
		}
		sc = append(sc, storageClass)
	}
	return sc
}

// applyAdditionalLabels applies additionalLabels from the matching DeviceClass to the StorageClass,
// and prunes any labels that were previously managed but have been removed from the CR.
func applyAdditionalLabels(ctx context.Context, sc *storagev1.StorageClass, cluster *lvmv1alpha1.LVMCluster, scName string) {
	logger := log.FromContext(ctx)
	if sc.Labels == nil {
		sc.Labels = make(map[string]string)
	}
	if sc.Annotations == nil {
		sc.Annotations = make(map[string]string)
	}

	dc := FindDeviceClassBySCName(scName, cluster.Spec.Storage.DeviceClasses)
	if dc == nil {
		// Fail-safe: don't prune/apply if we can't map SC -> DeviceClass
		logger.V(1).Info("cannot map StorageClass to DeviceClass, skipping additionalLabels reconciliation", "storageClass", scName)
		return
	}

	// Read previously managed label keys from annotation, trimming whitespace defensively
	var previousKeys []string
	if raw, ok := sc.Annotations[constants.ManagedAdditionalLabelsAnnotation]; ok && raw != "" {
		for _, k := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(k); trimmed != "" {
				previousKeys = append(previousKeys, trimmed)
			}
		}
	}

	currentAdditional := map[string]string{}
	if dc.StorageClassOptions != nil && dc.StorageClassOptions.AdditionalLabels != nil {
		currentAdditional = dc.StorageClassOptions.AdditionalLabels
	}

	// Remove previously managed labels that are no longer in additionalLabels,
	// but never prune operator-reserved keys even if a prior version tracked them.
	for _, key := range previousKeys {
		if strings.HasPrefix(key, labels.OwnedByPrefix) {
			continue
		}
		if _, reserved := constants.ReservedStorageClassLabelKeys[key]; reserved {
			continue
		}
		if _, stillPresent := currentAdditional[key]; !stillPresent {
			delete(sc.Labels, key)
		}
	}

	// Apply current additionalLabels, skipping operator-reserved label keys
	managedKeys := make([]string, 0, len(currentAdditional))
	for k, v := range currentAdditional {
		if strings.HasPrefix(k, labels.OwnedByPrefix) {
			logger.V(2).Info("additionalLabels key is reserved and will be ignored", "key", k, "storageClass", scName)
			continue
		}
		if _, reserved := constants.ReservedStorageClassLabelKeys[k]; reserved {
			logger.V(2).Info("additionalLabels key is reserved and will be ignored", "key", k, "storageClass", scName)
			continue
		}
		sc.Labels[k] = v
		managedKeys = append(managedKeys, strings.TrimSpace(k))
	}

	// Update tracking annotation
	if len(managedKeys) > 0 {
		sort.Strings(managedKeys)
		sc.Annotations[constants.ManagedAdditionalLabelsAnnotation] = strings.Join(managedKeys, ",")
	} else {
		delete(sc.Annotations, constants.ManagedAdditionalLabelsAnnotation)
	}
}
