package lvmcluster

import (
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ReasonReconciliationInProgress  = "ReconciliationInProgress"
	MessageReconciliationInProgress = "Reconciliation is still in progress"

	ReasonResourcesAvailable  = "ResourcesAvailable"
	MessageResourcesAvailable = "Reconciliation is complete and all the resources are available"

	ReasonResourcesSyncFailed        = "ResourcesSyncFailed"
	MessageReasonResourcesSyncFailed = "Reconciliation is failed with %v"

	ReasonVGReadinessInProgress  = "VGReadinessInProgress"
	MessageVGReadinessInProgress = "VG readiness check is still in progress"

	ReasonVGsFailed  = "VGsFailed"
	MessageVGsFailed = "One or more vgs are failed"

	ReasonVGsDegraded  = "VGsDegraded"
	MessageVGsDegraded = "One or more vgs are degraded"

	ReasonVGsReady  = "VGsReady"
	MessageVGsReady = "All the VGs are ready"

	ReasonVGsUnmanaged  = "VGsUnmanaged"
	MessageVGsUnmanaged = "VGs are unmanaged and not part of the LVMCluster, but the manager is running"
)

func setInitialConditions(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.ResourcesAvailable,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonReconciliationInProgress,
		Message: MessageReconciliationInProgress,
	})

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonVGReadinessInProgress,
		Message: MessageVGReadinessInProgress,
	})
}

func setResourcesAvailableConditionTrue(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.ResourcesAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonResourcesAvailable,
		Message: MessageResourcesAvailable,
	})
}

func setResourcesAvailableConditionFalse(instance *lvmv1alpha1.LVMCluster, reconcileError error) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.ResourcesAvailable,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonResourcesSyncFailed,
		Message: fmt.Sprintf(MessageReasonResourcesSyncFailed, reconcileError),
	})
}

func setVolumeGroupsReadyCondition(instance *lvmv1alpha1.LVMCluster, vgNodeStatusList *lvmv1alpha1.LVMVolumeGroupNodeStatusList) {
	reason := computeVolumeGroupsReadyReason(vgNodeStatusList)
	switch reason {
	case ReasonVGsReady:
		setVolumeGroupsReadyConditionTrue(instance)
	case ReasonVGsFailed:
		setVolumeGroupsReadyConditionFailed(instance)
	case ReasonVGsDegraded:
		setVolumeGroupsReadyConditionDegraded(instance)
	}
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

func setVolumeGroupsReadyConditionUnmanaged(instance *lvmv1alpha1.LVMCluster) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonVGsUnmanaged,
		Message: MessageVGsUnmanaged,
	})
}

func computeVolumeGroupsReadyReason(vgNodeStatusList *lvmv1alpha1.LVMVolumeGroupNodeStatusList) string {
	vgsCount := 0
	readyVGsCount := 0
	reason := ReasonVGReadinessInProgress
	for _, nodeItem := range vgNodeStatusList.Items {
		for _, vgStatus := range nodeItem.Spec.LVMVGStatus {
			vgsCount++
			switch vgStatus.Status {
			case lvmv1alpha1.VGStatusReady:
				readyVGsCount++
			case lvmv1alpha1.VGStatusFailed:
				return ReasonVGsFailed
			case lvmv1alpha1.VGStatusDegraded:
				reason = ReasonVGsDegraded
			}
		}
	}
	if readyVGsCount == vgsCount {
		return ReasonVGsReady
	}
	return reason
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
	case ReasonVGsFailed, ReasonResourcesSyncFailed:
		return lvmv1alpha1.LVMStatusFailed
	case ReasonVGsDegraded:
		if currentState != lvmv1alpha1.LVMStatusFailed {
			return lvmv1alpha1.LVMStatusDegraded
		}
	case ReasonReconciliationInProgress, ReasonVGReadinessInProgress:
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
