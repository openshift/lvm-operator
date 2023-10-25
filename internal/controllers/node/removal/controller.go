package removal

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const CleanupFinalizer = "lvm.topolvm.io/node-removal-hook"
const FieldOwner = "lvms"

type Reconciler struct {
	client.Client
}

func NewReconciler(client client.Client) *Reconciler {
	return &Reconciler{
		Client: client,
	}
}

//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lvm.topolvm.io,resources=lvmvolumegroupnodestatuses/finalizers,verbs=update

// Reconcile takes care of watching a node, adding a finalizer, and reacting to a removal request by deleting
// the unwanted LVMVolumeGroupNodeStatus that was associated with the node, before removing the finalizer.
// It does nothing on active Nodes. If it can be assumed that there will always be only one node (SNO),
// this controller should not be started.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	node := &v1.Node{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if node.DeletionTimestamp.IsZero() {
		// Add a finalizer in case the node is fresh or the controller newly deployed
		if needsUpdate := controllerutil.AddFinalizer(node, CleanupFinalizer); needsUpdate {
			if err := r.Update(ctx, node, client.FieldOwner(FieldOwner)); err != nil {
				return ctrl.Result{}, fmt.Errorf("node finalizer could not be updated: %w", err)
			}
		}
		// nothing to do here, the node exists and is happy,
		// maybe there is a NodeVolumeGroupStatus but we don't care
		return ctrl.Result{}, nil
	}

	logger.Info("node getting deleted, removing leftover LVMVolumeGroupNodeStatus")

	vgNodeStatusList := &lvmv1alpha1.LVMVolumeGroupNodeStatusList{}
	if err := r.Client.List(ctx, vgNodeStatusList, client.MatchingFields{"metadata.name": node.GetName()}); err != nil {
		return ctrl.Result{}, fmt.Errorf("error retrieving fitting LVMVolumeGroupNodeStatus for Node %s: %w", node.GetName(), err)
	}

	if len(vgNodeStatusList.Items) == 0 {
		logger.Info("LVMVolumeGroupNodeStatus already deleted")
		return ctrl.Result{}, nil
	}

	for i := range vgNodeStatusList.Items {
		if err := r.Client.Delete(ctx, &vgNodeStatusList.Items[i]); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not cleanup LVMVolumeGroupNodeStatus for Node %s: %w", node.GetName(), err)
		}
	}

	logger.Info("every LVMVolumeGroupNodeStatus for node was removed, removing finalizer to allow node removal")
	if needsUpdate := controllerutil.RemoveFinalizer(node, CleanupFinalizer); needsUpdate {
		if err := r.Update(ctx, node, client.FieldOwner(FieldOwner)); err != nil {
			return ctrl.Result{}, fmt.Errorf("node finalizer could not be removed: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1.Node{}).Watches(&lvmv1alpha1.LVMVolumeGroupNodeStatus{},
		handler.EnqueueRequestsFromMapFunc(r.GetNodeForLVMVolumeGroupNodeStatus)).Complete(r)
}

func (r *Reconciler) GetNodeForLVMVolumeGroupNodeStatus(ctx context.Context, object client.Object) []reconcile.Request {
	node := &v1.Node{}
	node.SetName(object.GetName())

	err := r.Get(ctx, client.ObjectKeyFromObject(node), node)
	if errors.IsNotFound(err) {
		return []reconcile.Request{}
	}

	if err != nil {
		log.FromContext(ctx).Error(err, "could not get Node for LVMVolumeGroupNodeStatus", "LVMVolumeGroupNodeStatus", object.GetName())
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: node.GetName()}}}
}
