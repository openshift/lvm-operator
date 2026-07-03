package vgmanager

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const RAIDMonitorName = "raid-monitor"

const (
	EventReasonRAIDDegraded      EventReasonError = "RAIDDegraded"
	EventReasonRAIDFailed        EventReasonError = "RAIDFailed"
	EventReasonRAIDRecovered     EventReasonInfo  = "RAIDRecovered"
	EventReasonRAIDSyncStarted   EventReasonInfo  = "RAIDSyncStarted"
	EventReasonRAIDSyncCompleted EventReasonInfo  = "RAIDSyncCompleted"
)

type RAIDMonitorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	events.EventRecorder
	lvm.LVM
	NodeName  string
	Namespace string
}

func (r *RAIDMonitorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(RAIDMonitorName).
		For(&lvmv1alpha1.LVMVolumeGroup{}).
		Complete(r)
}

func (r *RAIDMonitorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(RAIDMonitorName)

	volumeGroup := &lvmv1alpha1.LVMVolumeGroup{}
	if err := r.Get(ctx, req.NamespacedName, volumeGroup); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !volumeGroup.DeletionTimestamp.IsZero() {
		if volumeGroup.Spec.RAIDConfig != nil {
			deleteRAIDMetrics(r.NodeName, volumeGroup.GetName())
		}
		return ctrl.Result{}, nil
	}

	if volumeGroup.Spec.RAIDConfig == nil {
		return ctrl.Result{}, nil
	}

	nodeMatches, err := r.matchesThisNode(ctx, volumeGroup.Spec.NodeSelector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to match nodeSelector to node labels: %w", err)
	}
	if !nodeMatches {
		return ctrl.Result{}, nil
	}

	vgs, err := r.ListVGs(ctx, true)
	if err != nil {
		logger.Error(err, "failed to list volume groups for RAID health check")
		return reconcileAgain, nil
	}

	var targetVG *lvm.VolumeGroup
	for i := range vgs {
		if vgs[i].Name == volumeGroup.Name {
			targetVG = &vgs[i]
			break
		}
	}
	if targetVG == nil {
		return reconcileAgain, nil
	}

	totalPVs := len(targetVG.PVs)
	missingPVs := 0
	for _, pv := range targetVG.PVs {
		if pv.PvMissing != "" {
			missingPVs++
		}
	}

	lvReport, err := r.ListLVs(ctx, volumeGroup.GetName())
	if err != nil {
		logger.Error(err, "failed to list logical volumes for RAID health check")
		return reconcileAgain, nil
	}

	var allLVs []lvm.LogicalVolume
	for _, report := range lvReport.Report {
		allLVs = append(allLVs, report.Lv...)
	}

	newRAIDStatus := buildRAIDStatus(allLVs, volumeGroup.Spec.RAIDConfig.Type)
	if newRAIDStatus == nil && missingPVs > 0 {
		newRAIDStatus = &lvmv1alpha1.RAIDStatus{
			Status: lvmv1alpha1.RAIDHealthStatusDegraded,
		}
	}

	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.NodeName,
			Namespace: r.Namespace,
		},
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(nodeStatus), nodeStatus); err != nil {
		logger.Error(err, "failed to get LVMVolumeGroupNodeStatus")
		return reconcileAgain, nil
	}

	var oldRAIDStatus *lvmv1alpha1.RAIDStatus
	for _, vgStatus := range nodeStatus.Spec.LVMVGStatus {
		if vgStatus.Name == volumeGroup.GetName() {
			oldRAIDStatus = vgStatus.RAIDStatus
			break
		}
	}

	r.emitTransitionEvents(ctx, volumeGroup, oldRAIDStatus, newRAIDStatus)

	if statusChanged(oldRAIDStatus, newRAIDStatus) {
		if _, err := r.updateRAIDStatusOnNode(ctx, volumeGroup, nodeStatus, newRAIDStatus); err != nil {
			logger.Error(err, "failed to update RAID status on LVMVolumeGroupNodeStatus")
			return reconcileAgain, nil
		}
	}

	updateRAIDMetrics(r.NodeName, volumeGroup.GetName(), newRAIDStatus, totalPVs, missingPVs)

	return reconcileAgain, nil
}

func (r *RAIDMonitorReconciler) updateRAIDStatusOnNode(
	ctx context.Context,
	vg *lvmv1alpha1.LVMVolumeGroup,
	nodeStatus *lvmv1alpha1.LVMVolumeGroupNodeStatus,
	raidStatus *lvmv1alpha1.RAIDStatus,
) (bool, error) {
	result, err := ctrl.CreateOrUpdate(ctx, r.Client, nodeStatus, func() error {
		for i, vgStatus := range nodeStatus.Spec.LVMVGStatus {
			if vgStatus.Name == vg.GetName() {
				nodeStatus.Spec.LVMVGStatus[i].RAIDStatus = raidStatus
				if raidStatus != nil &&
					(raidStatus.Status == lvmv1alpha1.RAIDHealthStatusDegraded || raidStatus.Status == lvmv1alpha1.RAIDHealthStatusFailed) &&
					(vgStatus.Status == lvmv1alpha1.VGStatusReady || vgStatus.Status == lvmv1alpha1.VGStatusProgressing) {
					nodeStatus.Spec.LVMVGStatus[i].Status = lvmv1alpha1.VGStatusDegraded
				}
				if raidStatus != nil &&
					raidStatus.Status == lvmv1alpha1.RAIDHealthStatusHealthy &&
					vgStatus.Status == lvmv1alpha1.VGStatusDegraded {
					nodeStatus.Spec.LVMVGStatus[i].Status = lvmv1alpha1.VGStatusReady
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("failed to update LVMVolumeGroupNodeStatus: %w", err)
	}
	return result != controllerutil.OperationResultNone, nil
}

func (r *RAIDMonitorReconciler) emitTransitionEvents(
	ctx context.Context,
	vg *lvmv1alpha1.LVMVolumeGroup,
	oldStatus, newStatus *lvmv1alpha1.RAIDStatus,
) {
	oldOverall := overallStatus(oldStatus)
	newOverall := overallStatus(newStatus)

	if oldOverall != newOverall {
		switch newOverall {
		case lvmv1alpha1.RAIDHealthStatusDegraded:
			r.raidWarningEvent(ctx, vg, EventReasonRAIDDegraded,
				fmt.Errorf("RAID array %s on node %s is degraded", vg.GetName(), r.NodeName))
		case lvmv1alpha1.RAIDHealthStatusFailed:
			r.raidWarningEvent(ctx, vg, EventReasonRAIDFailed,
				fmt.Errorf("RAID array %s on node %s has failed", vg.GetName(), r.NodeName))
		case lvmv1alpha1.RAIDHealthStatusHealthy:
			if oldOverall != "" {
				r.raidNormalEvent(ctx, vg, EventReasonRAIDRecovered,
					fmt.Sprintf("RAID array %s on node %s has recovered", vg.GetName(), r.NodeName))
			}
		}
	}

	oldSyncing := syncingLVs(oldStatus)
	newSyncing := syncingLVs(newStatus)

	for lv := range newSyncing {
		if !oldSyncing[lv] {
			r.raidNormalEvent(ctx, vg, EventReasonRAIDSyncStarted,
				fmt.Sprintf("RAID LV %s in %s on node %s started resynchronization", lv, vg.GetName(), r.NodeName))
		}
	}
	for lv := range oldSyncing {
		if !newSyncing[lv] {
			r.raidNormalEvent(ctx, vg, EventReasonRAIDSyncCompleted,
				fmt.Sprintf("RAID LV %s in %s on node %s completed resynchronization", lv, vg.GetName(), r.NodeName))
		}
	}
}

func (r *RAIDMonitorReconciler) raidWarningEvent(ctx context.Context, obj *lvmv1alpha1.LVMVolumeGroup, reason EventReasonError, errMsg error) {
	emitEventToVGAndOwners(ctx, r.Client, r.EventRecorder, r.NodeName, r.Namespace, obj,
		corev1.EventTypeWarning, string(reason), "MonitorRAIDHealth", errMsg.Error())
}

func (r *RAIDMonitorReconciler) raidNormalEvent(ctx context.Context, obj *lvmv1alpha1.LVMVolumeGroup, reason EventReasonInfo, message string) {
	emitEventToVGAndOwners(ctx, r.Client, r.EventRecorder, r.NodeName, r.Namespace, obj,
		corev1.EventTypeNormal, string(reason), "MonitorRAIDHealth", message)
}

func (r *RAIDMonitorReconciler) matchesThisNode(ctx context.Context, selector *corev1.NodeSelector) (bool, error) {
	return matchesNode(ctx, r.Client, r.NodeName, selector)
}

func overallStatus(s *lvmv1alpha1.RAIDStatus) lvmv1alpha1.RAIDHealthStatus {
	if s == nil {
		return ""
	}
	return s.Status
}

func syncingLVs(s *lvmv1alpha1.RAIDStatus) map[string]bool {
	result := make(map[string]bool)
	if s == nil {
		return result
	}
	for _, lv := range s.LVHealth {
		if lv.SyncPercent < 100 {
			result[lv.Name] = true
		}
	}
	return result
}

func statusChanged(old, new *lvmv1alpha1.RAIDStatus) bool {
	if old == nil && new == nil {
		return false
	}
	if old == nil || new == nil {
		return true
	}
	if old.Status != new.Status {
		return true
	}
	if len(old.LVHealth) != len(new.LVHealth) {
		return true
	}
	for i := range old.LVHealth {
		if old.LVHealth[i].Name != new.LVHealth[i].Name ||
			old.LVHealth[i].SyncPercent != new.LVHealth[i].SyncPercent ||
			old.LVHealth[i].HealthStatus != new.LVHealth[i].HealthStatus {
			return true
		}
	}
	return false
}
