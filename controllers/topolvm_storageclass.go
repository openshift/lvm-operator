package controllers

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
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
	topolvmStorageClasses := getTopolvmStorageClasses(lvmCluster)
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
		scName := fmt.Sprintf("odf-lvm-%s", deviceClass.Name)
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

func (s topolvmStorageClass) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	// intentionally empty as there'll be no status field on StorageClass resource
	return nil
}

func getTopolvmStorageClasses(lvmCluster *lvmv1alpha1.LVMCluster) []*storagev1.StorageClass {
	sc := []*storagev1.StorageClass{}
	allowVolumeExpansion := true
	volumeBindingMode := storagev1.VolumeBindingWaitForFirstConsumer

	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		storageClass := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("odf-lvm-%s", deviceClass.Name),
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
		sc = append(sc, storageClass)
	}
	return sc
}
