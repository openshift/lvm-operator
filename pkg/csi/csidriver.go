package csi

import (
	"context"
	"github.com/coreos/pkg/capnslog"
	k8sUtils "github.com/red-hat-storage/lvm-operator/pkg/util/k8s"
	storageV1 "k8s.io/api/storage/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	csiDriverName = "topolvm.cybozu.com"
)

var (
	logger = capnslog.NewPackageLogger("github.com/red-hat-storage/lvm-operator", "csi")
)

// CreateCSIDriverInfo Registers CSI driver by creating a CSIDriver object
func CreateCSIDriverInfo(ctx context.Context) error {
	clientSet, err := k8sUtils.GetClientSet(ctx)
	if err != nil {
		logger.Errorf("Not possible to get a valid clientSet to add %s to CSIDrivers", csiDriverName)
		return err
	}
	attachRequired := false
	podInfoOnMount := true
	storageCapacity := true

	// Create CSIDriver object
	csiDriver := &storageV1.CSIDriver{
		ObjectMeta: metaV1.ObjectMeta{
			Name: csiDriverName,
		},
		Spec: storageV1.CSIDriverSpec{
			AttachRequired:       &attachRequired,
			PodInfoOnMount:       &podInfoOnMount,
			StorageCapacity:      &storageCapacity,
			VolumeLifecycleModes: []storageV1.VolumeLifecycleMode{storageV1.VolumeLifecyclePersistent, storageV1.VolumeLifecycleEphemeral},
		},
	}

	// Attach CSIDriver object
	csiDrivers := clientSet.StorageV1().CSIDrivers()
	driver, err := csiDrivers.Get(ctx, csiDriverName, metaV1.GetOptions{})
	if err != nil {
		if apiErrors.IsNotFound(err) {
			_, err = csiDrivers.Create(ctx, csiDriver, metaV1.CreateOptions{})
			if err != nil {
				return err
			}
			logger.Infof("CSIDriver object created for driver %q", csiDriverName)

			// For csiDriver we need to provide the resourceVersion when updating the object.
			// From the docs (https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata)
			// > "This value MUST be treated as opaque by clients and passed unmodified back to the server"
			csiDriver.ObjectMeta.ResourceVersion = driver.ObjectMeta.ResourceVersion
			_, err = csiDrivers.Update(ctx, csiDriver, metaV1.UpdateOptions{})
			if err != nil {
				return err
			}
			logger.Infof("CSIDriver object updated for driver %q", csiDriverName)
		}
		return err
	}

	return nil
}

// DeleteCSIDriverInfo deletes CSIDriverInfo and returns the error if any
func DeleteCSIDriverInfo(ctx context.Context) error {
	clientSet, err := k8sUtils.GetClientSet(ctx)
	if err != nil {
		logger.Errorf("Not possible to get a valid clientSet to remove %s from CSIDrivers", csiDriverName)
		return err
	}

	err = clientSet.StorageV1().CSIDrivers().Delete(ctx, csiDriverName, metaV1.DeleteOptions{})
	if err != nil {
		if apiErrors.IsNotFound(err) {
			logger.Debugf("%s CSIDriver not found; skipping deletion.", csiDriverName)
			return nil
		}
	}
	return err
}
