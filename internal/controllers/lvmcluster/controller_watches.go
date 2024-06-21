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

package lvmcluster

import (
	"context"

	snapapiv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	internalselector "github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/selector"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"

	secv1 "github.com/openshift/api/security/v1"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&lvmv1alpha1.LVMCluster{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.Deployment{}).
		Owns(&lvmv1alpha1.LVMVolumeGroup{}).
		Watches(
			&lvmv1alpha1.LVMVolumeGroupNodeStatus{},
			handler.EnqueueRequestsFromMapFunc(r.getLVMClusterObjsByNameFittingNodeSelector),
		).
		Watches(
			&storagev1.StorageClass{},
			handler.EnqueueRequestsFromMapFunc(r.getManagedLabelObjsForReconcile),
		)

	if r.ClusterType == cluster.TypeOCP {
		builder = builder.Watches(
			&secv1.SecurityContextConstraints{},
			handler.EnqueueRequestsFromMapFunc(r.getManagedLabelObjsForReconcile),
		)
	}
	if r.SnapshotsEnabled() {
		builder = builder.Watches(
			&snapapiv1.VolumeSnapshotClass{},
			handler.EnqueueRequestsFromMapFunc(r.getManagedLabelObjsForReconcile),
		)
	}

	return builder.Complete(r)
}

// getManagedLabelObjsForReconcile reconciles the object anytime the given object has all management labels
// set to the available lvmclusters. This can be used especially if owner references are not a valid option (e.g.
// the namespaced LVMCluster needs to "own" a cluster-scoped resource, in which case owner references are invalid).
// This should generally only be used for cluster-scoped resources. Also it should be noted that deletion logic must
// be handled manually as garbage collection is not handled automatically like for owner references.
func (r *Reconciler) getManagedLabelObjsForReconcile(ctx context.Context, obj client.Object) []reconcile.Request {
	foundLVMClusterList := &lvmv1alpha1.LVMClusterList{}
	listOps := &client.ListOptions{
		Namespace: obj.GetNamespace(),
	}

	if err := r.List(ctx, foundLVMClusterList, listOps); err != nil {
		log.FromContext(ctx).Error(err, "getManagedLabelObjsForReconcile: Failed to get LVMCluster objs")
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	for _, lvmCluster := range foundLVMClusterList.Items {
		if !labels.MatchesManagedLabels(r.Scheme(), obj, &lvmCluster) {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      lvmCluster.GetName(),
				Namespace: lvmCluster.GetNamespace(),
			},
		})
	}
	return requests
}

// getLVMClusterObjsByNameFittingNodeSelector enqueues the cluster in case the object name fits the node name.
// this means that as if the obj name fits to a given node on the cluster and that node is part of the node selector,
// then the lvm cluster will get updated as well. Should only be used in conjunction with LVMVolumeGroupNodeStatus
// as other objects do not use the node name as resource name.
func (r *Reconciler) getLVMClusterObjsByNameFittingNodeSelector(ctx context.Context, obj client.Object) []reconcile.Request {
	foundLVMClusterList := &lvmv1alpha1.LVMClusterList{}
	listOps := &client.ListOptions{
		Namespace: obj.GetNamespace(),
	}

	if err := r.List(ctx, foundLVMClusterList, listOps); err != nil {
		log.FromContext(ctx).Error(err, "getLVMClusterObjsByNameFittingNodeSelector: Failed to get LVMCluster objs")
		return []reconcile.Request{}
	}

	node := &v1.Node{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), node); err != nil {
		log.FromContext(ctx).Error(err, "getLVMClusterObjsByNameFittingNodeSelector: Failed to get Node")
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	for _, lvmCluster := range foundLVMClusterList.Items {
		selector, _ := internalselector.ExtractNodeSelectorAndTolerations(&lvmCluster)
		// if the selector is nil then the default behavior is to match all nodes
		if selector == nil {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      lvmCluster.GetName(),
					Namespace: lvmCluster.GetNamespace(),
				},
			})
			continue
		}

		match, err := corev1helper.MatchNodeSelectorTerms(node, selector)
		if err != nil {
			log.FromContext(ctx).Error(err, "getLVMClusterObjsByNameFittingNodeSelector: node selector matching in event handler failed")
			return []reconcile.Request{}
		}
		if match {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      lvmCluster.GetName(),
					Namespace: lvmCluster.GetNamespace(),
				},
			})
		}
	}
	return requests
}
