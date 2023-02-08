/*
Copyright 2021 Red Hat Openshift Data Foundation.

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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// SetupWithManager sets up the controller with the Manager.
func (r *LVMClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lvmv1alpha1.LVMCluster{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&lvmv1alpha1.LVMVolumeGroup{}).
		Owns(&lvmv1alpha1.LVMVolumeGroupNodeStatus{}).
		Watches(
			&source.Kind{Type: &lvmv1alpha1.LVMVolumeGroupNodeStatus{}},
			handler.EnqueueRequestsFromMapFunc(r.getLVMClusterObjsForReconcile),
			//			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *LVMClusterReconciler) getLVMClusterObjsForReconcile(obj client.Object) []reconcile.Request {
	foundLVMClusterList := &lvmv1alpha1.LVMClusterList{}
	listOps := &client.ListOptions{
		Namespace: obj.GetNamespace(),
	}

	err := r.Client.List(context.TODO(), foundLVMClusterList, listOps)
	if err != nil {
		r.Log.Error(err, "getLVMClusterObjsForReconcile: Failed to get LVMCluster objs")
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(foundLVMClusterList.Items))
	for i, item := range foundLVMClusterList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}
	return requests
}
