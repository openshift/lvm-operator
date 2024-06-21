package persistent_volume

import (
	"context"
	"strings"

	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Reconciler reconciles a PersistentVolume object
type Reconciler struct {
	client   client.Client
	recorder record.EventRecorder
}

// NewReconciler returns Reconciler.
func NewReconciler(client client.Client, eventRecorder record.EventRecorder) *Reconciler {
	return &Reconciler{
		client:   client,
		recorder: eventRecorder,
	}
}

//+kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile PV
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithName("pv-controller").WithValues("Request.Name", req.Name, "Request.Namespace", req.Namespace)

	pv := &corev1.PersistentVolume{}
	err := r.client.Get(ctx, req.NamespacedName, pv)
	switch {
	case err == nil:
	case apierrors.IsNotFound(err):
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, err
	}

	// Skip if the PV is deleted or PV does not use the lvms storage class.
	if pv.DeletionTimestamp != nil || !strings.HasPrefix(pv.Spec.StorageClassName, constants.StorageClassPrefix) {
		return ctrl.Result{}, nil
	}

	// Publish an event if PV has no claimRef
	if pv.Spec.ClaimRef == nil {
		r.recorder.Event(pv, "Warning", "ClaimReferenceRemoved", "Claim reference has been removed. This PV is no longer dynamically managed by LVM Storage and will need to be cleaned up manually.")
		logger.Info("Event published for the PV", "PV", req.NamespacedName)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(r.Predicates()).
		For(&corev1.PersistentVolume{}).
		Complete(r)
}

func (r *Reconciler) Predicates() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}
