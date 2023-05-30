package persistent_volume_claim

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/openshift/lvm-operator/controllers"
	"github.com/pkg/errors"

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
	capacityAnnotation = "capacity.topolvm.io/00default"
)

// PersistentVolumeClaimReconciler reconciles a PersistentVolumeClaim object
type PersistentVolumeClaimReconciler struct {
	client    client.Client
	apiReader client.Reader
	recorder  record.EventRecorder
}

// NewPersistentVolumeClaimReconciler returns PersistentVolumeClaimReconciler.
func NewPersistentVolumeClaimReconciler(client client.Client, apiReader client.Reader, eventRecorder record.EventRecorder) *PersistentVolumeClaimReconciler {
	return &PersistentVolumeClaimReconciler{
		client:    client,
		apiReader: apiReader,
		recorder:  eventRecorder,
	}
}

//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=core,resources=node,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile PVC
func (r *PersistentVolumeClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithName("persistentvolume-controller").WithValues("Request.Name", req.Name, "Request.Namespace", req.Namespace)

	pvc := &corev1.PersistentVolumeClaim{}
	err := r.client.Get(ctx, req.NamespacedName, pvc)
	switch {
	case err == nil:
	case apierrors.IsNotFound(err):
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, err
	}

	// Skip if the PVC is deleted or does not use the lvms storage class.
	if pvc.DeletionTimestamp != nil || !strings.HasPrefix(*pvc.Spec.StorageClassName, controllers.StorageClassPrefix) {
		return ctrl.Result{}, nil
	}

	// Skip if the PVC is not in Pending state.
	if pvc.Status.Phase != "Pending" {
		return ctrl.Result{}, nil
	}

	// List the nodes
	nodeList := &corev1.NodeList{}
	err = r.client.List(ctx, nodeList)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check if there is any node that has enough storage capacity for the PVC
	found := false
	requestedStorage := pvc.Spec.Resources.Requests.Storage()
	var nodeMessage []string
	for _, node := range nodeList.Items {
		capacity, ok := node.Annotations[capacityAnnotation]
		if !ok {
			errMessage := fmt.Sprintf("could not find capacity annotation on the node %s", node.Name)
			logger.Error(fmt.Errorf(errMessage), errMessage)
			return ctrl.Result{}, nil
		}

		capacityQuantity, err := resource.ParseQuantity(capacity)
		if err != nil {
			logger.Error(errors.Wrapf(err, "failed to parse capacity for node %s", node.Name), "failed to parse capacity")
			return ctrl.Result{}, nil
		}
		if requestedStorage.Cmp(capacityQuantity) < 0 {
			found = true
			break
		}
		nodeMessage = append(nodeMessage, fmt.Sprintf("node %s has %s free storage", node.Name, prettyByteSize(capacityQuantity.Value())))
	}

	// Publish an event if the requested storage is greater than the available capacity
	if !found {
		r.recorder.Event(pvc, "Warning", "NotEnoughCapacity",
			fmt.Sprintf("Requested storage (%s) is greater than available capacity on any node (%s).", requestedStorage.String(), strings.Join(nodeMessage, ",")))
		logger.Info("Event published for the PVC", "PVC", req.NamespacedName)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PersistentVolumeClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(pred).
		For(&corev1.PersistentVolumeClaim{}).
		Complete(r)
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
