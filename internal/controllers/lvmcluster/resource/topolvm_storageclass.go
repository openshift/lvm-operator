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

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	scName = "topolvm-storageclass"

	labelFieldOwner     = "lvms-labels"
	defaultSCAnnotation = "storageclass.kubernetes.io/is-default-class"
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

		// CreateOrUpdate handles immutable spec fields on creation only.
		result, err := cutil.CreateOrUpdate(ctx, r, sc, func() error {
			if sc.ResourceVersion == "" {
				sc.Provisioner = desired.Provisioner
				sc.VolumeBindingMode = desired.VolumeBindingMode
				sc.ReclaimPolicy = desired.ReclaimPolicy
				sc.AllowVolumeExpansion = desired.AllowVolumeExpansion
				sc.Parameters = make(map[string]string, len(desired.Parameters))
				maps.Copy(sc.Parameters, desired.Parameters)
			}

			// Default annotation is mutable — reconcile every time.
			if sc.Annotations == nil {
				sc.Annotations = make(map[string]string)
			}
			if desired.Annotations[defaultSCAnnotation] == "true" {
				sc.Annotations[defaultSCAnnotation] = "true"
			} else {
				delete(sc.Annotations, defaultSCAnnotation)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("%s failed to reconcile StorageClass %s: %w", s.GetName(), desired.Name, err)
		}
		if result != cutil.OperationResultNone {
			logger.V(2).Info("StorageClass reconciled", "operation", result, "name", sc.Name)
		}

		// SSA patch for labels — the field manager handles ownership and pruning automatically.
		// Provisioner is required by the StorageClass schema even in partial SSA objects.
		labelPatch := &storagev1.StorageClass{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "storage.k8s.io/v1",
				Kind:       "StorageClass",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   desired.Name,
				Labels: desired.Labels,
			},
			Provisioner: desired.Provisioner,
		}
		if err := r.Patch(ctx, labelPatch,
			client.Apply,
			client.FieldOwner(labelFieldOwner),
			client.ForceOwnership,
		); err != nil {
			if apierrors.IsConflict(err) {
				logger.Info("StorageClass label ownership conflict detected",
					"storageClass", desired.Name, "fieldOwner", labelFieldOwner, "error", err)
				return fmt.Errorf("StorageClass %q label conflict: field manager %q could not set one or more StorageClass label keys "+
					"because another field manager owns them. Remove the conflicting label key(s) from the StorageClass "+
					"or stop managing them outside LVMS (e.g. via GitOps/kubectl): %w", desired.Name, labelFieldOwner, err)
			}
			return fmt.Errorf("%s failed to apply labels for StorageClass %s: %w", s.GetName(), desired.Name, err)
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
			if apierrors.IsNotFound(err) {
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

		if deviceClass.StorageClassOptions != nil {
			maps.Copy(parameters, deviceClass.StorageClassOptions.AdditionalParameters)
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

		storageClass.Labels = make(map[string]string)
		labels.SetManagedLabels(r.Scheme(), storageClass, lvmCluster)
		if deviceClass.StorageClassOptions != nil {
			maps.Copy(storageClass.Labels, deviceClass.StorageClassOptions.AdditionalLabels)
		}

		sc = append(sc, storageClass)
	}
	return sc
}
