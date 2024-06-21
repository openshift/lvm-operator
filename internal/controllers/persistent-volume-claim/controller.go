package persistent_volume_claim

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	CapacityAnnotation = "capacity.topolvm.io/"
)

// Reconciler reconciles a PersistentVolumeClaim object
type Reconciler struct {
	Client   client.Client
	Recorder record.EventRecorder
}

// NewReconciler returns Reconciler.
func NewReconciler(client client.Client, eventRecorder record.EventRecorder) *Reconciler {
	return &Reconciler{
		Client:   client,
		Recorder: eventRecorder,
	}
}

//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=core,resources=node,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile PVC
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithName("pvc-controller").WithValues("Request.Name", req.Name, "Request.Namespace", req.Namespace)

	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Client.Get(ctx, req.NamespacedName, pvc)
	switch {
	case err == nil:
	case apierrors.IsNotFound(err):
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, err
	}

	// Skip if the PVC is deleted or does not use the lvms storage class.
	if pvc.DeletionTimestamp != nil {
		logger.Info("skipping pvc as it is about to be deleted (deletionTimestamp is set)")
		return ctrl.Result{}, nil
	}

	if pvc.Spec.StorageClassName == nil {
		logger.Info("skipping pvc as the storageClassName is not set")
		return ctrl.Result{}, nil
	}

	// Skip if StorageClassName does not contain the lvms prefix
	lvmsPrefix, deviceClass, exists := strings.Cut(*pvc.Spec.StorageClassName, "-")
	if !exists || fmt.Sprintf("%s-", lvmsPrefix) != constants.StorageClassPrefix {
		logger.Info("skipping pvc as the storageClassName does not contain desired prefix",
			"desired-prefix", constants.StorageClassPrefix)
		return ctrl.Result{}, nil
	}

	// Skip if the PVC is not in Pending state.
	if pvc.Status.Phase != "Pending" {
		return ctrl.Result{}, nil
	}

	// List the nodes
	nodeList := &corev1.NodeList{}
	err = r.Client.List(ctx, nodeList)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check if there is any node that has enough storage capacity for the PVC
	found := false
	requestedStorage := pvc.Spec.Resources.Requests.Storage()
	var nodeMessage []string
	for _, node := range nodeList.Items {
		capacity, ok := node.Annotations[CapacityAnnotation+deviceClass]
		if !ok {
			continue
		}

		capacityQuantity, err := resource.ParseQuantity(capacity)
		if err != nil {
			logger.Error(fmt.Errorf("failed to parse capacity for node %s: %w", node.Name, err), "failed to parse capacity")
			return ctrl.Result{}, nil
		}
		if requestedStorage.Cmp(capacityQuantity) < 0 {
			found = true
			break
		}
		nodeMessage = append(nodeMessage, fmt.Sprintf("node %s has %s free storage", node.Name, prettyByteSize(capacityQuantity.Value())))
	}

	// sort the message alphabetically so we always publish the same event
	sort.Strings(nodeMessage)

	// Publish an event if the requested storage is greater than the available capacity
	if !found {
		msg := fmt.Sprintf("Requested storage (%s) is greater than available capacity on any node (%s).", requestedStorage.String(), strings.Join(nodeMessage, ","))
		r.Recorder.Event(pvc, "Warning", "NotEnoughCapacity", msg)
		logger.V(7).Info(msg)
	}

	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(r.Predicates()).
		For(&corev1.PersistentVolumeClaim{}).
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

func prettyByteSize(b int64) string {
	bf := float64(b)
	for _, unit := range []string{"", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "Zi"} {
		if math.Abs(bf) < 1024.0 {
			return fmt.Sprintf("%3.1f%sB", bf, unit)
		}
		bf /= 1024.0
	}
	return fmt.Sprintf("%.1fYiB", bf)
}
