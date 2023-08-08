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
	"errors"
	"fmt"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	vscName = "topolvm-volumeSnapshotClass"
)

type topolvmVolumeSnapshotClass struct{}

// topolvmVolumeSnapshotClass unit satisfies resourceManager interface
var _ resourceManager = topolvmVolumeSnapshotClass{}

func (s topolvmVolumeSnapshotClass) getName() string {
	return vscName
}

//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;create;delete;watch;list

func (s topolvmVolumeSnapshotClass) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	// one volume snapshot class for every deviceClass based on CR is created
	topolvmSnapshotClasses := getTopolvmSnapshotClasses(lvmCluster)
	for _, vsc := range topolvmSnapshotClasses {

		// we anticipate no edits to volume snapshot class
		result, err := cutil.CreateOrUpdate(ctx, r.Client, vsc, func() error { return nil })
		if err != nil {
			r.Log.Error(err, "topolvm volume snapshot class reconcile failure", "name", vsc.Name)
			return err
		} else {
			r.Log.Info("topolvm volume snapshot class", "operation", result, "name", vsc.Name)
		}
	}
	return nil
}

func (s topolvmVolumeSnapshotClass) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	// construct name of volume snapshot class based on CR spec deviceClass field and
	// delete the corresponding volume snapshot class
	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		vsc := &snapapi.VolumeSnapshotClass{}
		vscName := getVolumeSnapshotClassName(deviceClass.Name)
		err := r.Client.Get(ctx, types.NamespacedName{Name: vscName}, vsc)

		if err != nil {
			// already deleted in previous reconcile
			if k8serrors.IsNotFound(err) {
				r.Log.Info("topolvm volume snapshot class is deleted", "VolumeSnapshotClass", vscName)
				return nil
			}
			if runtime.IsNotRegisteredError(err) || meta.IsNoMatchError(err) ||
				// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
				discovery.IsGroupDiscoveryFailedError(err) || discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
				r.Log.Info("topolvm volume snapshot classes do not exist on the cluster, ignoring", "VolumeSnapshotClass", vscName)
				return nil
			}
			r.Log.Error(err, "failed to retrieve topolvm volume snapshot class", "VolumeSnapshotClass", vscName)
			return err
		}

		// VolumeSnapshotClass exists, initiate deletion
		if vsc.GetDeletionTimestamp().IsZero() {
			if err = r.Client.Delete(ctx, vsc); err != nil {
				r.Log.Error(err, "failed to delete topolvm volume snapshot class", "VolumeSnapshotClass", vscName)
				return err
			} else {
				r.Log.Info("initiated topolvm volume snapshot class deletion", "VolumeSnapshotClass", vscName)
			}
		} else {
			// return error for next reconcile to confirm deletion
			return fmt.Errorf("topolvm volume snapshot class %s is already marked for deletion", vscName)
		}
	}
	return nil
}

func getTopolvmSnapshotClasses(lvmCluster *lvmv1alpha1.LVMCluster) []*snapapi.VolumeSnapshotClass {
	vsc := []*snapapi.VolumeSnapshotClass{}

	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		snapshotClass := &snapapi.VolumeSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: getVolumeSnapshotClassName(deviceClass.Name),
			},

			Driver:         TopolvmCSIDriverName,
			DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
		}
		vsc = append(vsc, snapshotClass)
	}
	return vsc
}
