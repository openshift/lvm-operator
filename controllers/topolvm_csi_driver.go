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
	driverName = "topolvm-csi-driver"
)

type csiDriver struct{}

// csiDriver unit satisfies resourceManager interface
var _ resourceManager = csiDriver{}

func (c csiDriver) getName() string {
	return driverName
}

//+kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;create;delete;watch;list

func (c csiDriver) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	csiDriverResource := getCSIDriverResource()
	result, err := cutil.CreateOrUpdate(ctx, r.Client, csiDriverResource, func() error {
		// no need to mutate any field
		return nil
	})

	if err != nil {
		r.Log.Error(err, "csi driver reconcile failure", "name", csiDriverResource.Name)
	} else {
		r.Log.Info("csi driver", "operation", result, "name", csiDriverResource.Name)
	}

	return err
}

func (c csiDriver) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	csiDriverResource := &storagev1.CSIDriver{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: TopolvmCSIDriverName}, csiDriverResource)

	if err != nil {
		// already deleted in previous reconcile
		if errors.IsNotFound(err) {
			r.Log.Info("csi driver deleted", "TopolvmCSIDriverName", csiDriverResource.Name)
			return nil
		}
		r.Log.Error(err, "failed to retrieve topolvm csi driver resource", "TopolvmCSIDriverName", csiDriverResource.Name)
		return err
	}

	// if not deleted, initiate deletion
	if csiDriverResource.GetDeletionTimestamp().IsZero() {
		if err = r.Client.Delete(ctx, csiDriverResource); err != nil {
			r.Log.Error(err, "failed to delete topolvm csi driver", "TopolvmCSIDriverName", csiDriverResource.Name)
			return err
		} else {
			r.Log.Info("initiated topolvm csi driver deletion", "TopolvmCSIDriverName", csiDriverResource.Name)
		}
	} else {
		// set deletion in-progress for next reconcile to confirm deletion
		return fmt.Errorf("topolvm csi driver %s is already marked for deletion", csiDriverResource.Name)
	}

	return err
}

func (c csiDriver) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	// intentionally empty as there'll be no status field on CSIDriver resource
	return nil
}

func getCSIDriverResource() *storagev1.CSIDriver {
	// topolvm doesn't use/need attacher and reduce a round trip of the rpc by setting this to false
	attachRequired := false
	podInfoOnMount := true

	// use storageCapacity tracking to take scheduling decisions
	storageCapacity := true
	csiDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: TopolvmCSIDriverName,
		},
		Spec: storagev1.CSIDriverSpec{
			AttachRequired:       &attachRequired,
			PodInfoOnMount:       &podInfoOnMount,
			StorageCapacity:      &storageCapacity,
			VolumeLifecycleModes: []storagev1.VolumeLifecycleMode{storagev1.VolumeLifecyclePersistent},
		},
	}
	return csiDriver
}
