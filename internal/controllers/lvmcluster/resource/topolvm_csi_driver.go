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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	csiDriverResource := getCSIDriverResource()

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
	return nil
}

func (c csiDriver) EnsureDeleted(r Reconciler, ctx context.Context, _ *lvmv1alpha1.LVMCluster) error {
	name := types.NamespacedName{Name: constants.TopolvmCSIDriverName}
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName(), "CSIDriver", constants.TopolvmCSIDriverName)
	csiDriverResource := &storagev1.CSIDriver{}
	if err := r.Get(ctx, name, csiDriverResource); err != nil {
		return client.IgnoreNotFound(err)
	}

	if !csiDriverResource.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("the CSIDriver %s is still present, waiting for deletion", constants.TopolvmCSIDriverName)
	}

	if err := r.Delete(ctx, csiDriverResource); err != nil {
		return fmt.Errorf("failed to delete topolvm csi driver %s: %w", csiDriverResource.GetName(), err)
	}
	logger.Info("initiated topolvm csi driver deletion", "TopolvmCSIDriverName", csiDriverResource.Name)

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
			Name: constants.TopolvmCSIDriverName,
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
