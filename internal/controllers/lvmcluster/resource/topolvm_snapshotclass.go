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

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	"k8s.io/apimachinery/pkg/api/errors"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	vscName = "topolvm-volumeSnapshotClass"
)

func TopoLVMVolumeSnapshotClass() Manager {
	return topolvmVolumeSnapshotClass{}
}

type topolvmVolumeSnapshotClass struct{}

// topolvmVolumeSnapshotClass unit satisfies resourceManager interface
var _ Manager = topolvmVolumeSnapshotClass{}

func (s topolvmVolumeSnapshotClass) GetName() string {
	return vscName
}

//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;create;delete;watch;list;update;patch

func (s topolvmVolumeSnapshotClass) EnsureCreated(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())
	// one volume snapshot class for every deviceClass based on CR is created
	topolvmSnapshotClasses := getTopolvmSnapshotClasses(cluster)
	for _, vsc := range topolvmSnapshotClasses {
		// we anticipate no edits to volume snapshot class
		result, err := cutil.CreateOrUpdate(ctx, r, vsc, func() error {
			labels.SetManagedLabels(r.Scheme(), vsc, cluster)
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s failed to reconcile: %w", s.GetName(), err)
		}
		if result != cutil.OperationResultNone {
			logger.V(2).Info("VolumeSnapshotClass applied to cluster", "operation", result, "name", vsc.Name)
		}
	}
	return nil
}

func (s topolvmVolumeSnapshotClass) EnsureDeleted(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.GetName())

	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		// construct name of volume snapshot class based on CR spec deviceClass field and
		// delete the corresponding volume snapshot class
		vscName := GetVolumeSnapshotClassName(deviceClass.Name)
		logger := logger.WithValues("VolumeSnapshotClass", vscName)

		vsc := &snapapi.VolumeSnapshotClass{}
		if err := r.Get(ctx, types.NamespacedName{Name: vscName}, vsc); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		if !vsc.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the VolumeSnapshotClass %s is still present, waiting for deletion", vscName)
		}

		if err := r.Delete(ctx, vsc); err != nil {
			return fmt.Errorf("failed to delete topolvm VolumeSnapshotClass %s: %w", vscName, err)
		}
		logger.Info("initiated VolumeSnapshotClass deletion")
	}
	return nil
}

func getTopolvmSnapshotClasses(lvmCluster *lvmv1alpha1.LVMCluster) []*snapapi.VolumeSnapshotClass {
	var vsc []*snapapi.VolumeSnapshotClass

	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		if deviceClass.ThinPoolConfig == nil {
			continue
		}
		snapshotClass := &snapapi.VolumeSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: GetVolumeSnapshotClassName(deviceClass.Name),
			},

			Driver:         constants.TopolvmCSIDriverName,
			DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
		}
		vsc = append(vsc, snapshotClass)
	}
	return vsc
}
