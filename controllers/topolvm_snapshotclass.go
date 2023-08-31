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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	logger := log.FromContext(ctx).WithValues("resourceManager", s.getName())
	// one volume snapshot class for every deviceClass based on CR is created
	topolvmSnapshotClasses := getTopolvmSnapshotClasses(lvmCluster)
	for _, vsc := range topolvmSnapshotClasses {
		// we anticipate no edits to volume snapshot class
		result, err := cutil.CreateOrUpdate(ctx, r.Client, vsc, func() error { return nil })
		if err != nil {
			// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
			if discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
				logger.Info("volume snapshot class CRDs do not exist on the cluster, ignoring", "VolumeSnapshotClass", vscName)
				return nil
			}
			return fmt.Errorf("%s failed to reconcile: %w", s.getName(), err)
		}
		logger.Info("VolumeSnapshotClass applied to cluster", "operation", result, "name", vsc.Name)
	}
	return nil
}

func (s topolvmVolumeSnapshotClass) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", s.getName())

	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		// construct name of volume snapshot class based on CR spec deviceClass field and
		// delete the corresponding volume snapshot class
		vscName := getVolumeSnapshotClassName(deviceClass.Name)
		logger := logger.WithValues("VolumeSnapshotClass", vscName)

		vsc := &snapapi.VolumeSnapshotClass{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: vscName}, vsc); err != nil {
			// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
			if discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
				logger.Info("VolumeSnapshotClasses do not exist on the cluster, ignoring")
				return nil
			}
			return client.IgnoreNotFound(err)
		}

		if !vsc.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the VolumeSnapshotClass %s is still present, waiting for deletion", vscName)
		}

		if err := r.Client.Delete(ctx, vsc); err != nil {
			return fmt.Errorf("failed to delete topolvm VolumeSnapshotClass %s: %w", vscName, err)
		}
		logger.Info("initiated VolumeSnapshotClass deletion")
	}
	return nil
}

func getTopolvmSnapshotClasses(lvmCluster *lvmv1alpha1.LVMCluster) []*snapapi.VolumeSnapshotClass {
	var vsc []*snapapi.VolumeSnapshotClass

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
