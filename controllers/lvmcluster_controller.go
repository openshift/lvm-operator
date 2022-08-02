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
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	v1helper "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var lvmClusterFinalizer = "lvmcluster.topolvm.io"

const (
	ControllerName = "lvmcluster-controller"

	openshiftSCCPrivilegedName = "privileged"
)

type ClusterType string

const (
	ClusterTypeOCP   ClusterType = "openshift"
	ClusterTypeOther ClusterType = "other"
)

// LVMClusterReconciler reconciles a LVMCluster object
type LVMClusterReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Log            logr.Logger
	ClusterType    ClusterType
	SecurityClient secv1client.SecurityV1Interface
	Namespace      string
	ImageName      string
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
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;create;update;delete
//+kubebuilder:rbac:groups=topolvm.cybozu.com,resources=logicalvolumes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

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
	r.Log = log.Log.WithName(ControllerName).WithValues("Request.Name", req.Name, "Request.Namespace", req.Namespace)
	r.Log.Info("reconciling")

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
	err = r.checkIfOpenshift(ctx)
	if err != nil {
		r.Log.Error(err, "failed to check cluster type")
		return ctrl.Result{}, err
	}

	err = r.getRunningPodImage(ctx)
	if err != nil {
		r.Log.Error(err, "failed to get operator image")
		return ctrl.Result{}, err
	}

	// verify node filter
	nodes := &corev1.NodeList{}
	err = r.Client.List(ctx, nodes)
	if err != nil {
		r.Log.Error(err, "failed to list nodes")
		return ctrl.Result{}, err
	}

	if len(nodes.Items) == 0 {
		return ctrl.Result{}, fmt.Errorf("no nodes are available")
	}

	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		var matches bool
		for i := range nodes.Items {
			matches, err = verifyNodeSelector(&nodes.Items[i], deviceClass.NodeSelector)
			if err != nil {
				r.Log.Error(err, "failed to verify node selector")
				return ctrl.Result{}, err
			}

			if matches {
				break
			}
		}

		// return error if none of the nodes match the nodeSelector for a deviceClass
		if !matches {
			r.Log.Error(err, "failed to get any matching nodes for the node selector", "deviceClass", deviceClass)
			return ctrl.Result{}, err
		}
	}

	result, reconcileError := r.reconcile(ctx, lvmCluster)

	statusError := r.updateLVMClusterStatus(ctx, lvmCluster)

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil && !errors.IsNotFound(statusError) {
		r.Log.Error(statusError, "failed to update LVMCluster status")
		return result, statusError
	} else {
		return result, nil
	}
}

// errors returned by this will be updated in the reconcileSucceeded condition of the LVMCluster
func (r *LVMClusterReconciler) reconcile(ctx context.Context, instance *lvmv1alpha1.LVMCluster) (ctrl.Result, error) {

	//The resource was deleted
	if !instance.DeletionTimestamp.IsZero() {
		// Check for existing LogicalVolumes
		lvsExist, err := r.logicalVolumesExist(ctx, instance)
		if err != nil {
			r.Log.Error(err, "failed to check if LogicalVolumes exist")
		} else {
			if lvsExist {
				err = fmt.Errorf("found PVCs provisioned by topolvm")
			} else {
				r.Log.Info("processing LVMCluster deletion")
				err = r.processDelete(ctx, instance)
			}
		}
		if err != nil {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute * 1}, err
		} else {
			return reconcile.Result{}, nil
		}
	}

	if !controllerutil.ContainsFinalizer(instance, lvmClusterFinalizer) {
		r.Log.Info("finalizer not found for LvmCluster. Adding finalizer")
		controllerutil.AddFinalizer(instance, lvmClusterFinalizer)
		if err := r.Client.Update(context.TODO(), instance); err != nil {
			r.Log.Error(err, "failed to update LvmCluster with finalizer")
			return reconcile.Result{}, err
		}
		r.Log.Info("successfully added finalizer")
	}

	resourceCreationList := []resourceManager{
		&csiDriver{},
		&topolvmController{},
		&openshiftSccs{},
		&topolvmNode{},
		&vgManager{},
		&lvmVG{},
		&topolvmStorageClass{},
		&topolvmVolumeSnapshotClass{},
	}

	// handle create/update
	for _, unit := range resourceCreationList {
		err := unit.ensureCreated(r, ctx, instance)
		if err != nil {
			r.Log.Error(err, "failed to reconcile", "resource", unit.getName())
			return ctrl.Result{}, err
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
	// ToDo: Change the status to something useful
	instance.Status.Ready = true

	r.Log.Info("successfully reconciled LvmCluster")

	return ctrl.Result{}, nil
}

func (r *LVMClusterReconciler) updateLVMClusterStatus(ctx context.Context, instance *lvmv1alpha1.LVMCluster) error {

	vgNodeMap := make(map[string][]lvmv1alpha1.NodeStatus)

	vgNodeStatusList := &lvmv1alpha1.LVMVolumeGroupNodeStatusList{}
	err := r.Client.List(ctx, vgNodeStatusList, client.InNamespace(r.Namespace))
	if err != nil {
		r.Log.Error(err, "failed to list LVMVolumeGroupNodeStatus")
		return err
	}

	for _, nodeItem := range vgNodeStatusList.Items {
		for _, item := range nodeItem.Spec.LVMVGStatus {
			val, ok := vgNodeMap[item.Name]
			if !ok {
				vgNodeMap[item.Name] = []lvmv1alpha1.NodeStatus{
					{
						Node:    nodeItem.Name,
						Status:  item.Status,
						Devices: item.Devices,
					},
				}
			} else {
				new := lvmv1alpha1.NodeStatus{Node: nodeItem.Name, Status: item.Status, Devices: item.Devices}
				val = append(val, new)
				vgNodeMap[item.Name] = val
			}
		}
	}

	allVgStatuses := []lvmv1alpha1.DeviceClassStatus{}
	for k := range vgNodeMap {
		//r.Log.Info("vgnode map ", "key", k, "NodeStatus", vgNodeMap[k])
		new := lvmv1alpha1.DeviceClassStatus{Name: k, NodeStatus: vgNodeMap[k]}
		allVgStatuses = append(allVgStatuses, new)
	}

	instance.Status.DeviceClassStatuses = allVgStatuses
	// Apply status changes
	err = r.Client.Status().Update(ctx, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Error(err, "failed to update status")
		}
		return err
	}

	r.Log.Info("successfully updated the LvmCluster status")

	return nil
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

// checkIfOpenshift checks to see if the operator is running on an OCP cluster.
// It does this by querying for the "privileged" SCC which exists on all OCP clusters.
func (r *LVMClusterReconciler) checkIfOpenshift(ctx context.Context) error {
	if r.ClusterType == "" {
		// cluster type has not been determined yet
		// Check if the privileged SCC exists on the cluster (this is one of the default SCCs)
		_, err := r.SecurityClient.SecurityContextConstraints().Get(ctx, openshiftSCCPrivilegedName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				// Not an Openshift cluster
				r.ClusterType = ClusterTypeOther
				r.Log.Info("openshiftSCC not found, setting cluster type to other")
			} else {
				// Something went wrong
				r.Log.Error(err, "failed to get SCC", "Name", openshiftSCCPrivilegedName)
				return err
			}
		} else {
			r.Log.Info("openshiftSCC found, setting cluster type to openshift")
			r.ClusterType = ClusterTypeOCP
		}
	}
	return nil
}

func IsOpenshift(r *LVMClusterReconciler) bool {
	return r.ClusterType == ClusterTypeOCP
}

// getRunningPodImage gets the operator image and set it in reconciler struct
func (r *LVMClusterReconciler) getRunningPodImage(ctx context.Context) error {

	if r.ImageName == "" {
		// 'POD_NAME' and 'POD_NAMESPACE' are set in env of lvm-operator when running as a container
		podName := os.Getenv("POD_NAME")
		if podName == "" {
			err := fmt.Errorf("failed to get pod name env variable")
			r.Log.Error(err, "POD_NAME env variable is not set")
			return err
		}

		pod := &corev1.Pod{}
		if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: r.Namespace}, pod); err != nil {
			r.Log.Error(err, "failed to get pod", "pod", podName)
			return err
		}

		for _, c := range pod.Spec.Containers {
			if c.Name == LVMOperatorContainerName {
				r.ImageName = c.Image
				return nil
			}
		}

		err := fmt.Errorf("failed to get container image for %s in pod %s", LVMOperatorContainerName, podName)
		r.Log.Error(err, "container image not found")
		return err

	}

	return nil
}

func (r *LVMClusterReconciler) logicalVolumesExist(ctx context.Context, instance *lvmv1alpha1.LVMCluster) (bool, error) {

	logicalVolumeList := &topolvmv1.LogicalVolumeList{}

	if err := r.Client.List(ctx, logicalVolumeList); err != nil {
		r.Log.Error(err, "failed to get Topolvm LogicalVolume list")
		return false, err
	}
	if len(logicalVolumeList.Items) > 0 {

		return true, nil
	}
	return false, nil
}

func (r *LVMClusterReconciler) processDelete(ctx context.Context, instance *lvmv1alpha1.LVMCluster) error {
	if controllerutil.ContainsFinalizer(instance, lvmClusterFinalizer) {

		resourceDeletionList := []resourceManager{
			&topolvmVolumeSnapshotClass{},
			&topolvmStorageClass{},
			&lvmVG{},
			&topolvmController{},
			&csiDriver{},
			&openshiftSccs{},
			&topolvmNode{},
			&vgManager{},
		}

		for _, unit := range resourceDeletionList {
			err := unit.ensureDeleted(r, ctx, instance)
			if err != nil {
				return fmt.Errorf("failed cleaning up: %s %w", unit.getName(), err)
			}
		}
		controllerutil.RemoveFinalizer(instance, lvmClusterFinalizer)
		if err := r.Client.Update(context.TODO(), instance); err != nil {
			r.Log.Info("failed to remove finalizer from LVMCluster", "LvmCluster", instance.Name)
			return err
		}
	}

	return nil
}

// verfiyNodeSelector returns true if node selector matches any node
func verifyNodeSelector(node *corev1.Node, nodeSelector *corev1.NodeSelector) (bool, error) {
	if nodeSelector == nil {
		return true, nil
	}
	if node == nil {
		return false, fmt.Errorf("the node var is nil")
	}

	return v1helper.MatchNodeSelectorTerms(node, nodeSelector)
}
