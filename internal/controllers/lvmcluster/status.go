package lvmcluster

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/selector"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ReasonReconciliationInProgress  = "ReconciliationInProgress"
	MessageReconciliationInProgress = "Reconciliation is still in progress"

	ReasonResourcesAvailable  = "ResourcesAvailable"
	MessageResourcesAvailable = "Reconciliation is complete and all the resources are available"

	ReasonResourcesIncomplete            = "ResourcesSyncIncomplete"
	MessageReasonResourcesSyncIncomplete = "Resources have not yet been fully synced to the cluster: %v"

	ReasonVGReadinessInProgress  = "VGReadinessInProgress"
	MessageVGReadinessInProgress = "VG readiness check is still in progress"

	ReasonVGsFailed  = "VGsFailed"
	MessageVGsFailed = "One or more VGs are failed"

	ReasonVGsDegraded  = "VGsDegraded"
	MessageVGsDegraded = "One or more VGs are degraded"

	ReasonVGsReady  = "VGsReady"
	MessageVGsReady = "All the VGs are ready"

	ReasonVGsUnmanaged  = "VGsUnmanaged"
	MessageVGsUnmanaged = "VGs are unmanaged and not part of the LVMCluster, but the manager is running"
)

func setResourcesAvailableConditionTrue(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.ResourcesAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonResourcesAvailable,
		Message: MessageResourcesAvailable,
	})
}

func setResourcesAvailableConditionFalse(instance *lvmv1alpha1.LVMCluster, resourceSyncError error) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.ResourcesAvailable,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonResourcesIncomplete,
		Message: fmt.Sprintf(MessageReasonResourcesSyncIncomplete, resourceSyncError),
	})
}

func setResourcesAvailableConditionInProgress(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.ResourcesAvailable,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonReconciliationInProgress,
		Message: MessageReconciliationInProgress,
	})
}

func setVolumeGroupsReadyConditionTrue(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonVGsReady,
		Message: MessageVGsReady,
	})
}

func setVolumeGroupsReadyConditionFailed(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonVGsFailed,
		Message: MessageVGsFailed,
	})
}

func setVolumeGroupsReadyConditionDegraded(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonVGsDegraded,
		Message: MessageVGsDegraded,
	})
}

func setVolumeGroupsReadyConditionInProgress(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonVGReadinessInProgress,
		Message: MessageVGReadinessInProgress,
	})
}

func setVolumeGroupsReadyConditionUnmanaged(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonVGsUnmanaged,
		Message: MessageVGsUnmanaged,
	})
}

func computeDeviceClassStatuses(vgNodeStatusList *lvmv1alpha1.LVMVolumeGroupNodeStatusList) []lvmv1alpha1.DeviceClassStatus {
	vgNodeMap := make(map[string][]lvmv1alpha1.NodeStatus)
	for _, nodeItem := range vgNodeStatusList.Items {
		for _, item := range nodeItem.Spec.LVMVGStatus {
			vgNodeMap[item.Name] = append(vgNodeMap[item.Name],
				lvmv1alpha1.NodeStatus{
					Node:     nodeItem.Name,
					VGStatus: *item.DeepCopy(),
				},
			)
		}
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

	return allVgStatuses
}

func computeLVMClusterReadiness(conditions []metav1.Condition) (lvmv1alpha1.LVMStateType, bool) {
	state := lvmv1alpha1.LVMStatusUnknown
	for _, c := range conditions {
		state = translateReasonToState(c.Reason, state)
	}
	if state == lvmv1alpha1.LVMStatusReady {
		return state, true
	}
	return state, false
}

func translateReasonToState(reason string, currentState lvmv1alpha1.LVMStateType) lvmv1alpha1.LVMStateType {
	switch reason {
	case ReasonVGsFailed:
		return lvmv1alpha1.LVMStatusFailed
	case ReasonVGsDegraded:
		if currentState != lvmv1alpha1.LVMStatusFailed {
			return lvmv1alpha1.LVMStatusDegraded
		}
	case ReasonReconciliationInProgress, ReasonVGReadinessInProgress, ReasonResourcesIncomplete:
		if currentState != lvmv1alpha1.LVMStatusFailed && currentState != lvmv1alpha1.LVMStatusDegraded {
			return lvmv1alpha1.LVMStatusProgressing
		}
	case ReasonResourcesAvailable, ReasonVGsReady, ReasonVGsUnmanaged:
		transitionToReadyAcceptable := true
		// if at least one other state was signalling Failed, Degraded or Progressing State,
		// we should not transition to Ready State. only if all other states are acceptable
		// we can transition to Ready State
		for _, unacceptableCurrentState := range []lvmv1alpha1.LVMStateType{
			lvmv1alpha1.LVMStatusFailed,
			lvmv1alpha1.LVMStatusDegraded,
			lvmv1alpha1.LVMStatusProgressing,
		} {
			if currentState == unacceptableCurrentState {
				transitionToReadyAcceptable = false
				break
			}
		}
		if transitionToReadyAcceptable {
			return lvmv1alpha1.LVMStatusReady
		}
	default:
		return lvmv1alpha1.LVMStatusUnknown
	}
	return currentState
}

func setVolumeGroupsReadyCondition(ctx context.Context, instance *lvmv1alpha1.LVMCluster, nodes *corev1.NodeList, vgNodeStatusList *lvmv1alpha1.LVMVolumeGroupNodeStatusList) {
	logger := log.FromContext(ctx)

	err := validateDeviceClassSetup(instance, nodes, vgNodeStatusList)
	if err == nil {
		setVolumeGroupsReadyConditionTrue(instance)
		return
	} else {
		logger.Error(err, "failed to validate device class setup")
	}

	degraded := false
	for _, nodeItem := range vgNodeStatusList.Items {
		for _, vgStatus := range nodeItem.Spec.LVMVGStatus {
			switch vgStatus.Status {
			case lvmv1alpha1.VGStatusFailed:
				setVolumeGroupsReadyConditionFailed(instance)
				return
			case lvmv1alpha1.VGStatusDegraded:
				degraded = true
			}
		}
	}

	if degraded {
		setVolumeGroupsReadyConditionDegraded(instance)
	}
}

func validateDeviceClassSetup(cluster *lvmv1alpha1.LVMCluster, nodes *corev1.NodeList, nodeStatusList *lvmv1alpha1.LVMVolumeGroupNodeStatusList) error {
	for _, deviceClass := range cluster.Spec.Storage.DeviceClasses {
		validNodeExists := false
		for _, node := range nodes.Items {
			valid, err := isNodeValid(&node, cluster, &deviceClass)
			if err != nil {
				return err
			}
			if !valid {
				continue
			}

			// If we reach this point, the node is valid, so check for the NodeStatus
			validNodeExists = true
			var relatedNodeStatus *lvmv1alpha1.LVMVolumeGroupNodeStatus
			for _, nodeStatus := range nodeStatusList.Items {
				if node.Name == nodeStatus.Name {
					relatedNodeStatus = &nodeStatus
					break
				}
			}

			// If no node status is found, return an error, we assume it should have been
			// created by the LVMCluster controller
			if relatedNodeStatus == nil {
				return fmt.Errorf("no node status found for node %s,"+
					"that is part of the expected nodes for device class %s",
					node.Name, deviceClass.Name)
			}

			// Check if the VGStatus for the device class is present in the NodeStatus
			var relatedVGStatus *lvmv1alpha1.VGStatus
			for _, vgStatus := range relatedNodeStatus.Spec.LVMVGStatus {
				if vgStatus.Name == deviceClass.Name {
					relatedVGStatus = &vgStatus
					break
				}
			}

			// If no VGStatus is found, return an error, we assume it should have been
			// created by the vgmanager
			if relatedVGStatus == nil {
				return fmt.Errorf("no VGStatus found for VG %s on node %s,"+
					"that is part of the expected nodes for device class %s",
					deviceClass.Name, node.Name, deviceClass.Name)
			}

			if relatedVGStatus.Status != lvmv1alpha1.VGStatusReady {
				return fmt.Errorf("VG %s on node %s is not in ready state (%s),"+
					"that is part of the expected nodes for device class %s",
					deviceClass.Name, relatedVGStatus.Status, node.Name, deviceClass.Name)
			}
		}
		if !validNodeExists {
			return fmt.Errorf("no valid node found for device class %s",
				deviceClass.Name)
		}
	}

	return nil
}

func isNodeValid(node *corev1.Node, cluster *lvmv1alpha1.LVMCluster, deviceClass *lvmv1alpha1.DeviceClass) (bool, error) {
	// Check if node tolerates all taints
	ok, err := selector.ToleratesAllTaints(node.Spec.Taints, cluster.Spec.Tolerations)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	// If no node selector is specified, the node is valid
	// If there is one, make sure the node matches the terms
	if deviceClass.NodeSelector != nil {
		if matches, err := corev1helper.MatchNodeSelectorTerms(node, deviceClass.NodeSelector); err != nil {
			return false, fmt.Errorf("error matching node selector terms: %v", err)
		} else if !matches {
			return false, nil
		}
	}

	return true, nil
}
