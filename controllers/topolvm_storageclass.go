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

package controllers

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	// one storage class for every deviceClass based on CR is created
	topolvmStorageClasses := getTopolvmStorageClasses(r, ctx, lvmCluster)
	for _, sc := range topolvmStorageClasses {

		// we anticipate no edits to storage class
		result, err := cutil.CreateOrUpdate(ctx, r.Client, sc, func() error { return nil })
		if err != nil {
			r.Log.Error(err, "topolvm storage class reconcile failure", "name", sc.Name)
			return err
		} else {
			r.Log.Info("topolvm storage class", "operation", result, "name", sc.Name)
		}
	}
	return nil
}

func (s topolvmStorageClass) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	// construct name of storage class based on CR spec deviceClass field and
	// delete the corresponding storage class
	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		sc := &storagev1.StorageClass{}
		scName := getStorageClassName(deviceClass.Name)
		err := r.Client.Get(ctx, types.NamespacedName{Name: scName}, sc)

		if err != nil {
			// already deleted in previous reconcile
			if errors.IsNotFound(err) {
				r.Log.Info("topolvm storage class is deleted", "StorageClass", scName)
				return nil
			}
			r.Log.Error(err, "failed to retrieve topolvm storage class", "StorageClass", scName)
			return err
		}

		// storageClass exists, initiate deletion
		if sc.GetDeletionTimestamp().IsZero() {
			if err = r.Client.Delete(ctx, sc); err != nil {
				r.Log.Error(err, "failed to delete topolvm storage class", "StorageClass", scName)
				return err
			} else {
				r.Log.Info("initiated topolvm storage class deletion", "StorageClass", scName)
			}
		} else {
			// return error for next reconcile to confirm deletion
			return fmt.Errorf("topolvm storage class %s is already marked for deletion", scName)
		}
	}
	return nil
}

func getTopolvmStorageClasses(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) []*storagev1.StorageClass {

	const defaultSCAnnotation string = "storageclass.kubernetes.io/is-default-class"
	allowVolumeExpansion := true
	volumeBindingMode := storagev1.VolumeBindingWaitForFirstConsumer
	defaultStorageClassName := ""
	setDefaultStorageClass := true

	// Mark the lvms storage class, associated with the default device class, as default if no other default storage class exists on the cluster
	scList := &storagev1.StorageClassList{}
	err := r.Client.List(ctx, scList)

	if err != nil {
		r.Log.Error(err, "failed to list storage classes. Not setting any storageclass as the default")
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
	sc := []*storagev1.StorageClass{}
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
				"csi.storage.k8s.io/fstype": TopolvmFilesystemType,
			},
		}
		// reconcile will pick up any existing LVMO storage classes as well
		if setDefaultStorageClass && (defaultStorageClassName == "" || defaultStorageClassName == scName) {
			if len(lvmCluster.Spec.Storage.DeviceClasses) == 1 || deviceClass.Default {
				storageClass.Annotations[defaultSCAnnotation] = "true"
				defaultStorageClassName = scName
			}
		}
		sc = append(sc, storageClass)
	}
	return sc
}
