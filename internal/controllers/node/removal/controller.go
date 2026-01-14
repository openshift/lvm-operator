package removal

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Reconciler struct {
	client.Client
	Namespace string
}

func NewReconciler(client client.Client, namespace string) *Reconciler {
	return &Reconciler{
		Client:    client,
		Namespace: namespace,
	}
}

//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses/finalizers,verbs=update
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroups,verbs=get;list;watch;update;patch

// Reconcile takes care of watching a LVMVolumeGroupNodeStatus, and reacting to a node removal request by deleting
// the unwanted LVMVolumeGroupNodeStatus that was associated with the node, as well as cleaning up any finalizers
// and annotations on LVMVolumeGroups that reference the deleted node.
// It does nothing on active Nodes. If it can be assumed that there will always be only one node (SNO),
// this controller should not be started.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{}
	if err := r.Get(ctx, req.NamespacedName, nodeStatus); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.V(2).Info("verifying if node status is orphaned", "node", nodeStatus.GetName())

	node := &v1.Node{}
	err := r.Get(ctx, client.ObjectKeyFromObject(nodeStatus), node)

	if errors.IsNotFound(err) {
		nodeName := nodeStatus.GetName()
		logger.Info("node not found, cleaning up node resources", "node", nodeName)

		// Clean up finalizers and annotations for this specific node
		if err := r.cleanupNodeFinalizers(ctx, nodeName); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to cleanup finalizers for node %s: %w", nodeName, err)
		}

		if err := r.cleanupNodeAnnotations(ctx, nodeName); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to cleanup annotations for node %s: %w", nodeName, err)
		}

		// Delete the NodeStatus CR
		if err := r.Delete(ctx, nodeStatus); err != nil {
			return ctrl.Result{}, fmt.Errorf("error deleting LVMVolumeGroupNodeStatus for Node %s: %w", nodeName, err)
		}
		logger.Info("initiated LVMVolumeGroupNodeStatus deletion", "nodeStatus", client.ObjectKeyFromObject(nodeStatus))

		if removeDeleteProtectionFinalizer(nodeStatus) {
			if err := r.Update(ctx, nodeStatus); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from LVMVolumeGroupNodeStatus: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error retrieving fitting LVMVolumeGroupNodeStatus for Node %s: %w", nodeStatus.GetName(), err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lvmv1alpha1.LVMVolumeGroupNodeStatus{}).
		Watches(&v1.Node{}, handler.EnqueueRequestsFromMapFunc(r.GetNodeStatusFromNode)).
		WithOptions(controller.Options{SkipNameValidation: ptr.To(true)}).
		Complete(r)
}

// GetNodeStatusFromNode can be used to derive a reconcile.Request from a Node for a NodeStatus.
// It does this by translating the type and injecting the namespace.
// Thus, whenever a node is updated, also the nodestatus will be checked.
// This makes sure that our removal controller is able to successfully reconcile on all node removals.
func (r *Reconciler) GetNodeStatusFromNode(ctx context.Context, object client.Object) []reconcile.Request {
	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{}
	nodeStatus.SetName(object.GetName())
	nodeStatus.SetNamespace(r.Namespace)
	nodeStatusKey := client.ObjectKeyFromObject(nodeStatus)

	err := r.Get(ctx, nodeStatusKey, nodeStatus)
	if errors.IsNotFound(err) {
		return []reconcile.Request{}
	}
	if err != nil {
		log.FromContext(ctx).Error(err, "could not getNode LVMVolumeGroupNodeStatus after Node was updated", "LVMVolumeGroupNodeStatus", object.GetName())
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: nodeStatusKey}}
}

func removeDeleteProtectionFinalizer(status *lvmv1alpha1.LVMVolumeGroupNodeStatus) bool {
	finalizers := status.GetFinalizers()
	for i, finalizer := range finalizers {
		if finalizer == constants.DeleteProtectionFinalizer {
			status.SetFinalizers(append(finalizers[:i], finalizers[i+1:]...))
			return true
		}
	}
	return false
}

// cleanupNodeFinalizers removes finalizers for the deleted node from all LVMVolumeGroups
func (r *Reconciler) cleanupNodeFinalizers(ctx context.Context, nodeName string) error {
	logger := log.FromContext(ctx)

	volumeGroups := &lvmv1alpha1.LVMVolumeGroupList{}
	if err := r.List(ctx, volumeGroups, &client.ListOptions{Namespace: r.Namespace}); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to list volume groups: %w", err)
	}

	finalizerToRemove := vgmanager.NodeCleanupFinalizer + "/" + nodeName

	for _, vg := range volumeGroups.Items {
		if controllerutil.ContainsFinalizer(&vg, finalizerToRemove) {
			controllerutil.RemoveFinalizer(&vg, finalizerToRemove)
			if err := r.Update(ctx, &vg); err != nil {
				return fmt.Errorf("failed to remove finalizer from volumegroup %s: %w", vg.Name, err)
			}
			logger.Info("removed node finalizer from VolumeGroup",
				"volumeGroup", vg.Name,
				"node", nodeName)
		}
	}

	return nil
}

// cleanupNodeAnnotations removes wiped-devices annotations for the deleted node from all LVMVolumeGroups
func (r *Reconciler) cleanupNodeAnnotations(ctx context.Context, nodeName string) error {
	logger := log.FromContext(ctx)

	volumeGroups := &lvmv1alpha1.LVMVolumeGroupList{}
	if err := r.List(ctx, volumeGroups, &client.ListOptions{Namespace: r.Namespace}); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to list volume groups: %w", err)
	}

	annotationKey := constants.DevicesWipedAnnotationPrefix + nodeName

	for _, vg := range volumeGroups.Items {
		if vg.Annotations == nil {
			continue
		}

		if _, exists := vg.Annotations[annotationKey]; exists {
			delete(vg.Annotations, annotationKey)
			if err := r.Update(ctx, &vg); err != nil {
				return fmt.Errorf("failed to remove annotation from volumegroup %s: %w", vg.Name, err)
			}
			logger.Info("removed wiped-devices annotation from VolumeGroup",
				"volumeGroup", vg.Name,
				"node", nodeName,
				"annotation", annotationKey)
		}
	}

	return nil
}
