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

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	driverName = "topolvm-csi-driver"
)

func CSIDriver() Manager {
	return csiDriver{}
}

type csiDriver struct{}

// csiDriver unit satisfies resourceManager interface
var _ Manager = csiDriver{}

func (c csiDriver) GetName() string {
	return driverName
}

//+kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;create;delete;watch;list;update;patch

func (c csiDriver) EnsureCreated(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName())
	csiDriverResources := getCSIDriverResources(cluster)

	for _, csiDriverResource := range csiDriverResources {
		result, err := cutil.CreateOrUpdate(ctx, r, csiDriverResource, func() error {
			labels.SetManagedLabels(r.Scheme(), csiDriverResource, cluster)
			// no need to mutate any field
			return nil
		})

		if err != nil {
			return fmt.Errorf("%s failed to reconcile: %w", c.GetName(), err)
		}
		if result != cutil.OperationResultNone {
			logger.V(2).Info("CSIDriver applied to cluster", "operation", result, "name", csiDriverResource.Name)
		}
	}

	return nil
}

func (c csiDriver) EnsureDeleted(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName(), "CSIDriver", constants.TopolvmCSIDriverName)

	csiDriverResources := getCSIDriverResources(cluster)

	for _, csiDriverResource := range csiDriverResources {
		name := client.ObjectKeyFromObject(csiDriverResource)
		if err := r.Get(ctx, name, csiDriverResource); err != nil {
			return client.IgnoreNotFound(err)
		}

		if !csiDriverResource.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the CSIDriver %s is still present, waiting for deletion", constants.TopolvmCSIDriverName)
		}

		if err := r.Delete(ctx, csiDriverResource); errors.IsNotFound(err) {
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to delete topolvm csi driver %s: %w", csiDriverResource.GetName(), err)
		}

		logger.Info("initiated CSIDriver deletion", "TopolvmCSIDriverName", csiDriverResource.Name)

		if err := r.Get(ctx, name, csiDriverResource); errors.IsNotFound(err) {
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to verify deletion of topolvm csi driver %s: %w", csiDriverResource.GetName(), err)
		} else {
			return fmt.Errorf("topolvm csi driver %s still has to be removed", csiDriverResource.Name)
		}
	}

	return nil
}

func getCSIDriverResources(cluster *lvmv1alpha1.LVMCluster) []*storagev1.CSIDriver {
	// topolvm doesn't use/need attacher and reduce a round trip of the rpc by setting this to false
	attachRequired := false
	podInfoOnMount := true

	drivers := make([]*storagev1.CSIDriver, 0, 2)

	shared, standard := RequiresSharedVolumeGroupSetup(cluster.Spec.Storage.DeviceClasses)

	if shared {
		// use storageCapacity tracking to take scheduling decisions
		drivers = append(drivers, &storagev1.CSIDriver{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.KubeSANCSIDriverName,
			},
			Spec: storagev1.CSIDriverSpec{
				AttachRequired:       &attachRequired,
				PodInfoOnMount:       &podInfoOnMount,
				StorageCapacity:      ptr.To(true),
				VolumeLifecycleModes: []storagev1.VolumeLifecycleMode{storagev1.VolumeLifecyclePersistent},
			},
		})
	}

	if standard {
		// use storageCapacity tracking to take scheduling decisions
		drivers = append(drivers, &storagev1.CSIDriver{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.TopolvmCSIDriverName,
			},
			Spec: storagev1.CSIDriverSpec{
				AttachRequired:       &attachRequired,
				PodInfoOnMount:       &podInfoOnMount,
				StorageCapacity:      ptr.To(true),
				VolumeLifecycleModes: []storagev1.VolumeLifecycleMode{storagev1.VolumeLifecyclePersistent},
			},
		})
	}

	return drivers
}
