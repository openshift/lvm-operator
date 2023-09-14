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
	"os"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/pkg/cluster"

	topolvmv1 "github.com/topolvm/topolvm/api/v1"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type EventReasonInfo string
type EventReasonError string

const (
	EventReasonErrorDeletionPending              EventReasonError = "DeletionPending"
	EventReasonErrorResourceReconciliationFailed EventReasonError = "ResourceReconciliationFailed"
	EventReasonResourceReconciliationSuccess     EventReasonInfo  = "ResourceReconciliationSuccess"

	lvmClusterFinalizer = "lvmcluster.topolvm.io"
)

// NOTE: when updating this, please also update docs/design/lvm-operator-manager.md
type resourceManager interface {

	// getName should return a camelCase name of this unit of reconciliation
	getName() string

	// ensureCreated should check the resources managed by this unit
	ensureCreated(*LVMClusterReconciler, context.Context, *lvmv1alpha1.LVMCluster) error

	// ensureDeleted should wait for the resources to be cleaned up
	ensureDeleted(*LVMClusterReconciler, context.Context, *lvmv1alpha1.LVMCluster) error
}

// LVMClusterReconciler reconciles a LVMCluster object
type LVMClusterReconciler struct {
	client.Client
	record.EventRecorder
	Scheme             *runtime.Scheme
	ClusterType        cluster.Type
	EnableSnapshotting bool
	Namespace          string
	ImageName          string

	// TopoLVMLeaderElectionPassthrough uses the given leaderElection when initializing TopoLVM to synchronize
	// leader election configuration
	TopoLVMLeaderElectionPassthrough configv1.LeaderElection
}

//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroups/finalizers,verbs=update
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses/finalizers,verbs=update
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get
//+kubebuilder:rbac:groups=topolvm.io,resources=logicalvolumes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *LVMClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling")

	// Checks that only a single LVMCluster instance exists
	lvmClusterList := &lvmv1alpha1.LVMClusterList{}
	if err := r.Client.List(context.TODO(), lvmClusterList, &client.ListOptions{}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list LVMCluster instances: %w", err)
	}
	if size := len(lvmClusterList.Items); size > 1 {
		return ctrl.Result{}, fmt.Errorf("there should be a single LVMCluster but multiple were found, %d clusters found", size)
	}

	// get lvmcluster
	lvmCluster := &lvmv1alpha1.LVMCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, lvmCluster); err != nil {
		// Error reading the object - requeue the request unless not found.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.setRunningPodImage(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to introspect running pod image: %w", err)
	}

	result, reconcileError := r.reconcile(ctx, lvmCluster)

	statusError := r.updateLVMClusterStatus(ctx, lvmCluster)

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil && !k8serrors.IsNotFound(statusError) {
		return result, fmt.Errorf("failed to update LVMCluster status: %w", statusError)
	} else {
		return result, nil
	}
}

// errors returned by this will be updated in the reconcileSucceeded condition of the LVMCluster
func (r *LVMClusterReconciler) reconcile(ctx context.Context, instance *lvmv1alpha1.LVMCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// The resource was deleted
	if !instance.DeletionTimestamp.IsZero() {
		// Check for existing LogicalVolumes
		lvsExist, err := r.logicalVolumesExist(ctx)
		if err != nil {
			// check every 10 seconds if there are still PVCs present
			return ctrl.Result{}, fmt.Errorf("failed to check if LogicalVolumes exist: %w", err)
		}
		if lvsExist {
			waitForLVRemoval := time.Second * 10
			err := fmt.Errorf("found PVCs provisioned by topolvm, waiting %s for their deletion", waitForLVRemoval)
			r.WarningEvent(ctx, instance, EventReasonErrorDeletionPending, err)
			// check every 10 seconds if there are still PVCs present
			return ctrl.Result{RequeueAfter: waitForLVRemoval}, nil
		}

		logger.Info("processing LVMCluster deletion")
		if err := r.processDelete(ctx, instance); err != nil {
			// check every 10 seconds if there are still PVCs present or the LogicalVolumes are removed
			return ctrl.Result{Requeue: true}, fmt.Errorf("failed to process LVMCluster deletion: %w", err)
		}
		return reconcile.Result{}, nil
	}

	if updated := controllerutil.AddFinalizer(instance, lvmClusterFinalizer); updated {
		if err := r.Client.Update(ctx, instance); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update LvmCluster with finalizer: %w", err)
		}
		logger.Info("successfully added finalizer")
	}

	resources := []resourceManager{
		&csiDriver{},
		&topolvmController{},
		&topolvmNode{},
		&vgManager{},
		&lvmVG{},
		&topolvmStorageClass{},
	}

	if r.ClusterType == cluster.TypeOCP {
		resources = append(resources, openshiftSccs{})
	}

	if r.EnableSnapshotting {
		resources = append(resources, &topolvmVolumeSnapshotClass{})
	}

	resourceSyncStart := time.Now()
	results := make(chan error, len(resources))
	create := func(i int) {
		results <- resources[i].ensureCreated(r, ctx, instance)
	}

	for i := range resources {
		go create(i)
	}

	var errs []error
	for i := 0; i < len(resources); i++ {
		if err := <-results; err != nil {
			errs = append(errs, err)
		}
	}

	resourceSyncElapsedTime := time.Since(resourceSyncStart)
	if len(errs) > 0 {
		err := fmt.Errorf("failed to reconcile resources managed by LVMCluster: %w", errors.Join(errs...))
		r.WarningEvent(ctx, instance, EventReasonErrorResourceReconciliationFailed, err)
		return ctrl.Result{}, err
	}

	msg := "successfully reconciled LVMCluster"
	logger.Info(msg, "resourceSyncElapsedTime", resourceSyncElapsedTime)
	r.NormalEvent(ctx, instance, EventReasonResourceReconciliationSuccess, msg)

	return ctrl.Result{}, nil
}

func (r *LVMClusterReconciler) updateLVMClusterStatus(ctx context.Context, instance *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx)

	vgNodeMap := make(map[string][]lvmv1alpha1.NodeStatus)

	vgNodeStatusList := &lvmv1alpha1.LVMVolumeGroupNodeStatusList{}
	if err := r.Client.List(ctx, vgNodeStatusList, client.InNamespace(r.Namespace)); err != nil {
		return fmt.Errorf("failed to list LVMVolumeGroupNodeStatus: %w", err)
	}

	expectedVGCount, err := r.getExpectedVGCount(ctx, instance)
	if err != nil {
		return fmt.Errorf("failed to calculate expected VG count: %w", err)
	}

	var readyVGCount int
	var isReady, isDegraded, isFailed bool

	for _, nodeItem := range vgNodeStatusList.Items {
		for _, item := range nodeItem.Spec.LVMVGStatus {
			if item.Status == lvmv1alpha1.VGStatusReady {
				readyVGCount++
				isReady = true
			} else if item.Status == lvmv1alpha1.VGStatusDegraded {
				isDegraded = true
			} else if item.Status == lvmv1alpha1.VGStatusFailed {
				isFailed = true
			}

			vgNodeMap[item.Name] = append(vgNodeMap[item.Name],
				lvmv1alpha1.NodeStatus{
					Node:    nodeItem.Name,
					Reason:  item.Reason,
					Status:  item.Status,
					Devices: item.Devices,
				},
			)
		}
	}

	instance.Status.State = lvmv1alpha1.LVMStatusProgressing
	instance.Status.Ready = false

	logger.Info("calculating readiness of LVMCluster", "expectedVGCount", expectedVGCount, "readyVGCount", readyVGCount)

	if isFailed {
		instance.Status.State = lvmv1alpha1.LVMStatusFailed
	} else if isDegraded {
		instance.Status.State = lvmv1alpha1.LVMStatusDegraded
	} else if isReady && expectedVGCount == readyVGCount {
		instance.Status.State = lvmv1alpha1.LVMStatusReady
		instance.Status.Ready = true
	}

	var allVgStatuses []lvmv1alpha1.DeviceClassStatus
	for key, val := range vgNodeMap {
		allVgStatuses = append(allVgStatuses,
			lvmv1alpha1.DeviceClassStatus{
				Name:       key,
				NodeStatus: val,
			},
		)
	}

	instance.Status.DeviceClassStatuses = allVgStatuses
	// Apply status changes
	if err = r.Client.Status().Update(ctx, instance); err != nil {
		return fmt.Errorf("failed to update LVMCluster status: %w", err)
	}
	logger.Info("successfully updated the LVMCluster status")
	return nil
}

func (r *LVMClusterReconciler) getExpectedVGCount(ctx context.Context, instance *lvmv1alpha1.LVMCluster) (int, error) {
	logger := log.FromContext(ctx)
	var vgCount int

	nodeList := &corev1.NodeList{}
	if err := r.Client.List(ctx, nodeList); err != nil {
		return 0, fmt.Errorf("failed to list Nodes: %w", err)
	}

	for _, deviceClass := range instance.Spec.Storage.DeviceClasses {
		for i := range nodeList.Items {
			ignoreDueToNoSchedule := false
			for _, taint := range nodeList.Items[i].Spec.Taints {
				if taint.Effect == corev1.TaintEffectNoSchedule {
					logger.V(1).Info("even though node selector matches, NoSchedule forces ignore of the Node",
						"node", nodeList.Items[i].GetName())
					ignoreDueToNoSchedule = true
					break
				}
			}
			if ignoreDueToNoSchedule {
				continue
			}

			if deviceClass.NodeSelector == nil {
				vgCount++
				continue
			}

			matches, err := corev1helper.MatchNodeSelectorTerms(&nodeList.Items[i], deviceClass.NodeSelector)
			if err != nil {
				return 0, fmt.Errorf("failed to match node selector: %w", err)
			}

			if matches {
				vgCount++
			}
		}
	}

	return vgCount, nil
}

// getRunningPodImage gets the operator image and set it in reconciler struct
func (r *LVMClusterReconciler) setRunningPodImage(ctx context.Context) error {

	if r.ImageName == "" {
		// 'POD_NAME' and 'POD_NAMESPACE' are set in env of lvm-operator when running as a container
		podName := os.Getenv(PodNameEnv)
		if podName == "" {
			return fmt.Errorf("failed to get pod name env variable, %s env variable is not set", PodNameEnv)
		}

		pod := &corev1.Pod{}
		if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: r.Namespace}, pod); err != nil {
			return fmt.Errorf("failed to get pod %s: %w", podName, err)
		}

		for _, c := range pod.Spec.Containers {
			if c.Name == LVMOperatorContainerName {
				r.ImageName = c.Image
				return nil
			}
		}

		return fmt.Errorf("failed to get container image for %s in pod %s", LVMOperatorContainerName, podName)
	}

	return nil
}

func (r *LVMClusterReconciler) logicalVolumesExist(ctx context.Context) (bool, error) {
	logicalVolumeList := &topolvmv1.LogicalVolumeList{}
	if err := r.Client.List(ctx, logicalVolumeList); err != nil {
		return false, fmt.Errorf("failed to get TopoLVM LogicalVolume list: %w", err)
	}
	if len(logicalVolumeList.Items) > 0 {
		return true, nil
	}
	return false, nil
}

func (r *LVMClusterReconciler) processDelete(ctx context.Context, instance *lvmv1alpha1.LVMCluster) error {
	if controllerutil.ContainsFinalizer(instance, lvmClusterFinalizer) {
		resourceDeletionList := []resourceManager{
			&topolvmStorageClass{},
			&lvmVG{},
			&topolvmController{},
			&csiDriver{},
			&topolvmNode{},
			&vgManager{},
		}

		if r.ClusterType == cluster.TypeOCP {
			resourceDeletionList = append(resourceDeletionList, openshiftSccs{})
		}

		if r.EnableSnapshotting {
			resourceDeletionList = append(resourceDeletionList, &topolvmVolumeSnapshotClass{})
		}

		for _, unit := range resourceDeletionList {
			if err := unit.ensureDeleted(r, ctx, instance); err != nil {
				err := fmt.Errorf("failed cleaning up %s: %w", unit.getName(), err)
				r.WarningEvent(ctx, instance, EventReasonErrorDeletionPending, err)
				return err
			}
		}
	}

	if update := controllerutil.RemoveFinalizer(instance, lvmClusterFinalizer); update {
		if err := r.Client.Update(ctx, instance); err != nil {
			return fmt.Errorf("failed to remove finalizer from LVMCluster %s: %w", instance.GetName(), err)
		}
	}

	return nil
}

func (r *LVMClusterReconciler) WarningEvent(_ context.Context, obj client.Object, reason EventReasonError, err error) {
	r.Event(obj, corev1.EventTypeWarning, string(reason), err.Error())
}

func (r *LVMClusterReconciler) NormalEvent(ctx context.Context, obj client.Object, reason EventReasonInfo, message string) {
	if !log.FromContext(ctx).V(1).Enabled() {
		return
	}
	r.Event(obj, corev1.EventTypeNormal, string(reason), message)
}
