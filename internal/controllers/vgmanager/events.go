package vgmanager

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func emitEventToVGAndOwners(
	ctx context.Context,
	c client.Reader,
	recorder events.EventRecorder,
	nodeName, namespace string,
	obj *lvmv1alpha1.LVMVolumeGroup,
	eventType, reason, action, message string,
) {
	nodeKey := client.ObjectKey{Name: nodeName, Namespace: namespace}
	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{}
	nodeStatus.SetName(nodeName)
	nodeStatus.SetNamespace(namespace)
	if err := c.Get(ctx, client.ObjectKeyFromObject(nodeStatus), nodeStatus); err == nil {
		recorder.Eventf(nodeStatus, nil, eventType, reason, action, message)
	}
	for _, ref := range obj.GetOwnerReferences() {
		owner := &metav1.PartialObjectMetadata{}
		owner.SetName(ref.Name)
		owner.SetNamespace(obj.GetNamespace())
		owner.SetUID(ref.UID)
		owner.SetGroupVersionKind(schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind))
		if eventType == corev1.EventTypeWarning {
			recorder.Eventf(owner, nil, eventType, reason, action,
				fmt.Sprintf("error on node %s in volume group %s: %s", nodeKey, client.ObjectKeyFromObject(obj), message))
		} else {
			recorder.Eventf(owner, nil, eventType, reason, action,
				fmt.Sprintf("update on node %s in volume group %s: %s", nodeKey, client.ObjectKeyFromObject(obj), message))
		}
	}
	if eventType == corev1.EventTypeWarning {
		recorder.Eventf(obj, nil, eventType, reason, action,
			fmt.Sprintf("error on node %s: %s", nodeKey, message))
	} else {
		recorder.Eventf(obj, nil, eventType, reason, action,
			fmt.Sprintf("update on node %s: %s", nodeKey, message))
	}
}
