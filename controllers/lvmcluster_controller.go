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
	"strings"

	"github.com/go-logr/logr"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ControllerName = "lvmcluster-controller"
)

// LVMClusterReconciler reconciles a LVMCluster object
type LVMClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info("reconciling", "topolvmcluster", req)
	result, err := r.reconcile(ctx, req, logger)
	// TODO: update status with condition describing whether reconcile succeeded
	if err != nil {
		logger.Error(err, "reconcile error")
	}

	return result, err
}

// errors returned by this will be updated in the reconcileSucceeded condition of the LVMCluster
func (r *LVMClusterReconciler) reconcile(ctx context.Context, req ctrl.Request, logger logr.Logger) (ctrl.Result, error) {
	result := ctrl.Result{}

	// get lvmcluster
	lvmCluster := &lvmv1alpha1.LVMCluster{}
	err := r.Client.Get(ctx, req.NamespacedName, lvmCluster)
	if err != nil {
		return result, fmt.Errorf("failed to fetch lvmCluster: %w", err)
	}

	unitList := []reconcileUnit{}

	// handle deletion
	if !lvmCluster.DeletionTimestamp.IsZero() {
		for _, unit := range unitList {
			err := unit.ensureDeleted(r, *lvmCluster)
			if err != nil {
				return result, fmt.Errorf("failed cleaning up: %s %w", unit.getDescription(), err)
			}
		}
	}

	// handle create/update
	for _, unit := range unitList {
		err := unit.ensureCreated(r, *lvmCluster)
		if err != nil {
			return result, fmt.Errorf("failed reconciling: %s %w", unit.getDescription(), err)
		}
	}

	// check  and report deployment status
	var failedStatusUpdates []string
	var lastError error
	for _, unit := range unitList {
		err := unit.updateStatus(r, *lvmCluster)
		if err != nil {
			failedStatusUpdates = append(failedStatusUpdates, unit.getDescription())
			unitError := fmt.Errorf("failed updating status for: %s %w", unit.getDescription(), err)
			logger.Error(unitError, "")
		}
	}
	// return simple message that will fit in status reconcileSucceeded condition, don't put all the errors there
	if len(failedStatusUpdates) > 0 {
		return ctrl.Result{}, fmt.Errorf("status update failed for %s: %w", strings.Join(failedStatusUpdates, ","), lastError)
	}

	return ctrl.Result{}, nil

}

type reconcileUnit interface {
	getDescription() string
	ensureCreated(*LVMClusterReconciler, lvmv1alpha1.LVMCluster) error
	ensureDeleted(*LVMClusterReconciler, lvmv1alpha1.LVMCluster) error
	// each unit will have updateStatus called induvidually so
	// avoid status fields like lastHeartbeatTime and have a
	// status that changes only when the operands change.
	updateStatus(*LVMClusterReconciler, lvmv1alpha1.LVMCluster) error
}
