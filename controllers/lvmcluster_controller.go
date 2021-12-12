/*
Copyright 2021.

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
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/go-logr/logr"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var lvmClusterFinalizer = "lvmcluster.topolvm.io"

const (
	ControllerName = "lvmcluster-controller"
)

// LVMClusterReconciler reconciles a LVMCluster object
type LVMClusterReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Log             logr.Logger
	DevelopmentMode bool
}

//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the LVMCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *LVMClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log = log.FromContext(ctx).WithName(ControllerName)
	r.Log.Info("reconciling", "lvmcluster", req)

	// get lvmcluster
	lvmCluster := &lvmv1alpha1.LVMCluster{}
	err := r.Client.Get(ctx, req.NamespacedName, lvmCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("lvmCluster instance not found")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	result, reconcileError := r.reconcile(ctx, lvmCluster)

	// Apply status changes
	statusError := r.Client.Status().Update(ctx, lvmCluster)
	if statusError != nil {
		if errors.IsNotFound(err) {
			r.Log.Error(statusError, "failed to update status")
		}
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil && errors.IsNotFound(statusError) {
		return result, statusError
	} else {
		return result, nil
	}
}

// errors returned by this will be updated in the reconcileSucceeded condition of the LVMCluster
func (r *LVMClusterReconciler) reconcile(ctx context.Context, instance *lvmv1alpha1.LVMCluster) (ctrl.Result, error) {
	resourceList := []resourceManager{
		&csiDriver{},
		vgManager{},
	}

	//The resource was deleted
	if !instance.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(instance, lvmClusterFinalizer) {
			for _, unit := range resourceList {
				err := unit.ensureDeleted(r, ctx, instance)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed cleaning up: %s %w", unit.getName(), err)
				}
			}
			controllerutil.RemoveFinalizer(instance, lvmClusterFinalizer)
			if err := r.Client.Update(context.TODO(), instance); err != nil {
				r.Log.Info("failed to remove finalizer from LvmCluster", "LvmCluster", instance.Name)
				return reconcile.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(instance, lvmClusterFinalizer) {
		r.Log.Info("Finalizer not found for LvmCluster. Adding finalizer.", "LvmCluster", instance.Name)
		controllerutil.AddFinalizer(instance, lvmClusterFinalizer)
		if err := r.Client.Update(context.TODO(), instance); err != nil {
			r.Log.Info("failed to update LvmCluster with finalizer.", "LvmCluster", instance.Name)
			return reconcile.Result{}, err
		}
	}

	// handle create/update
	for _, unit := range resourceList {
		r.Log.Info("running resourceManager", "resourceManager", unit.getName())
		err := unit.ensureCreated(r, ctx, instance)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed reconciling: %s %w", unit.getName(), err)
		}
	}

	/* 	// check  and report deployment status
	   	var failedStatusUpdates []string
	   	var lastError error
	   	for _, unit := range resourceList {
	   		err := unit.updateStatus(r, ctx, instance)
	   		if err != nil {
	   			failedStatusUpdates = append(failedStatusUpdates, unit.getName())
	   			unitError := fmt.Errorf("failed updating status for: %s %w", unit.getName(), err)
	   			r.Log.Error(unitError, "")
	   		}
	   	} */
	/* 	// return simple message that will fit in status reconcileSucceeded condition, don't put all the errors there
	   	if len(failedStatusUpdates) > 0 {
	   		return ctrl.Result{}, fmt.Errorf("status update failed for %s: %w", strings.Join(failedStatusUpdates, ","), lastError)
	   	}
	*/
	//ToDo: Change the status to something useful
	instance.Status.Ready = true
	return ctrl.Result{}, nil
}

// NOTE: when updating this, please also update doc/design/operator.md
type resourceManager interface {

	// getName should return a camelCase name of this unit of reconciliation
	getName() string

	// ensureCreated should check the resources managed by this unit
	ensureCreated(*LVMClusterReconciler, context.Context, *lvmv1alpha1.LVMCluster) error

	// ensureDeleted should wait for the resources to be cleaned up
	ensureDeleted(*LVMClusterReconciler, context.Context, *lvmv1alpha1.LVMCluster) error

	// updateStatus should optionally update the CR's status about the health of the managed resource
	// each unit will have updateStatus called individually so
	// avoid status fields like lastHeartbeatTime and have a
	// status that changes only when the operands change.
	updateStatus(*LVMClusterReconciler, context.Context, *lvmv1alpha1.LVMCluster) error
}
