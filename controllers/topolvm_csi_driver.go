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

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/pkg/errors"
	storagev1 "k8s.io/api/storage/v1"
	k8serror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	driverName = "topolvm-csi-driver"
)

type csiDriver struct {
	*runtime.Scheme
}

// csiDriver unit satisfies resourceManager interface
var _ resourceManager = csiDriver{}

func (c csiDriver) getName() string {
	return driverName
}

//+kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;create;delete;watch;list

func (c csiDriver) ensureCreated(r *LVMClusterReconciler, ctx context.Context, _ *lvmv1alpha1.LVMCluster) error {
	driver := getCSIDriverResource()

	if versioned, err := c.Scheme.ConvertToVersion(driver,
		runtime.GroupVersioner(schema.GroupVersions(c.Scheme.PrioritizedVersionsAllGroups()))); err == nil {
		driver = versioned.(*storagev1.CSIDriver)
	}

	err := r.Client.Patch(ctx, driver, client.Apply, client.ForceOwnership, client.FieldOwner(ControllerName))

	if err != nil {
		r.Log.Error(err, "csi driver reconcile failure", "name", driver.Name)
		return err
	}

	r.Log.Info("csi driver", "name", driver.Name)
	return nil
}

func (c csiDriver) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	csiDriverResource := &storagev1.CSIDriver{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: TopolvmCSIDriverName}, csiDriverResource)

	if err != nil {
		// already deleted in previous reconcile
		if k8serror.IsNotFound(err) {
			r.Log.Info("csi driver deleted", "TopolvmCSIDriverName", csiDriverResource.Name)
			return nil
		}
		r.Log.Error(err, "failed to retrieve topolvm csi driver resource", "TopolvmCSIDriverName", csiDriverResource.Name)
		return err
	}

	// if not deleted, initiate deletion
	if !csiDriverResource.GetDeletionTimestamp().IsZero() {
		// set deletion in-progress for next reconcile to confirm deletion
		return errors.Errorf("topolvm csi driver %s is already marked for deletion", csiDriverResource.Name)
	}

	err = r.Client.Delete(ctx, csiDriverResource)
	if err != nil {
		r.Log.Error(err, "failed to delete topolvm csi driver", "TopolvmCSIDriverName", csiDriverResource.Name)
		return err
	}
	r.Log.Info("initiated topolvm csi driver deletion", "TopolvmCSIDriverName", csiDriverResource.Name)

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
