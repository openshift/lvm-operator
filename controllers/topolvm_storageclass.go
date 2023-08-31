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

package controllers

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	scName = "topolvm-storageclass"
)

type topolvmStorageClass struct{}

// topolvmStorageClass unit satisfies resourceManager interface
var _ resourceManager = topolvmStorageClass{}

func (s topolvmStorageClass) getName() string {
	return scName
}

//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;create;delete;watch;list

func (s topolvmStorageClass) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.getName())

	// one storage class for every deviceClass based on CR is created
	topolvmStorageClasses := s.getTopolvmStorageClasses(r, ctx, lvmCluster)
	for _, sc := range topolvmStorageClasses {
		// we anticipate no edits to storage class
		result, err := cutil.CreateOrUpdate(ctx, r.Client, sc, func() error { return nil })
		if err != nil {
			return fmt.Errorf("%s failed to reconcile: %w", s.getName(), err)
		}
		logger.Info("StorageClass applied to cluster", "operation", result, "name", sc.Name)
	}
	return nil
}

func (s topolvmStorageClass) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.getName())

	// construct name of storage class based on CR spec deviceClass field and
	// delete the corresponding storage class
	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		scName := getStorageClassName(deviceClass.Name)
		logger := logger.WithValues("StorageClass", scName)

		sc := &storagev1.StorageClass{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: scName}, sc); err != nil {
			return client.IgnoreNotFound(err)
		}

		if !sc.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the StorageClass %s is still present, waiting for deletion", scName)
		}

		if err := r.Client.Delete(ctx, sc); err != nil {
			return fmt.Errorf("failed to delete StorageClass %s: %w", scName, err)
		}
		logger.Info("initiated StorageClass deletion")
	}
	return nil
}

func (s topolvmStorageClass) getTopolvmStorageClasses(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) []*storagev1.StorageClass {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.getName())

	const defaultSCAnnotation string = "storageclass.kubernetes.io/is-default-class"
	allowVolumeExpansion := true
	volumeBindingMode := storagev1.VolumeBindingWaitForFirstConsumer
	defaultStorageClassName := ""
	setDefaultStorageClass := true

	// Mark the lvms storage class, associated with the default device class, as default if no other default storage class exists on the cluster
	scList := &storagev1.StorageClassList{}
	err := r.Client.List(ctx, scList)

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
		scName := getStorageClassName(deviceClass.Name)

		storageClass := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: scName,
				Annotations: map[string]string{
					"description": "Provides RWO and RWOP Filesystem & Block volumes",
				},
			},
			Provisioner:          TopolvmCSIDriverName,
			VolumeBindingMode:    &volumeBindingMode,
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters: map[string]string{
				DeviceClassKey:              deviceClass.Name,
				"csi.storage.k8s.io/fstype": string(deviceClass.FilesystemType),
			},
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
