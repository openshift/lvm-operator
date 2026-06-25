package vgmanager

import (
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	raidHealthStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lvms_raid_health_status",
			Help: "RAID health status for a device class. 0=healthy, 1=degraded, 2=failed.",
		},
		[]string{"node", "device_class"},
	)

	raidSyncInProgress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lvms_raid_sync_in_progress",
			Help: "Whether any RAID LV in the device class is resynchronizing. 1=syncing, 0=idle.",
		},
		[]string{"node", "device_class"},
	)
)

// RAIDMetrics returns the Prometheus collectors for RAID health and sync status.
func RAIDMetrics() []prometheus.Collector {
	return []prometheus.Collector{raidHealthStatus, raidSyncInProgress}
}

// updateRAIDMetrics sets the RAID health and sync-in-progress gauges for a device class on a node.
func updateRAIDMetrics(nodeName, deviceClassName string, raidStatus *lvmv1alpha1.RAIDStatus) {
	if raidStatus == nil {
		raidHealthStatus.WithLabelValues(nodeName, deviceClassName).Set(0)
		raidSyncInProgress.WithLabelValues(nodeName, deviceClassName).Set(0)
		return
	}

	var healthValue float64
	switch raidStatus.Status {
	case lvmv1alpha1.RAIDHealthStatusHealthy:
		healthValue = 0
	case lvmv1alpha1.RAIDHealthStatusDegraded:
		healthValue = 1
	case lvmv1alpha1.RAIDHealthStatusFailed:
		healthValue = 2
	}
	raidHealthStatus.WithLabelValues(nodeName, deviceClassName).Set(healthValue)

	var syncing float64
	for _, lv := range raidStatus.LVHealth {
		if lv.SyncPercent < 100 {
			syncing = 1
			break
		}
	}
	raidSyncInProgress.WithLabelValues(nodeName, deviceClassName).Set(syncing)
}

// deleteRAIDMetrics removes the RAID metric series for a device class on a node.
func deleteRAIDMetrics(nodeName, deviceClassName string) {
	raidHealthStatus.DeleteLabelValues(nodeName, deviceClassName)
	raidSyncInProgress.DeleteLabelValues(nodeName, deviceClassName)
}
