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

	raidMemberCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lvms_raid_member_count",
			Help: "Total number of physical volumes in the RAID volume group.",
		},
		[]string{"node", "device_class"},
	)

	raidDegradedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lvms_raid_degraded_count",
			Help: "Number of missing or degraded physical volumes in the RAID volume group.",
		},
		[]string{"node", "device_class"},
	)

	raidSyncPercent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lvms_raid_sync_percent",
			Help: "Minimum resynchronization percentage across all RAID LVs in the device class (0-100).",
		},
		[]string{"node", "device_class"},
	)
)

// RAIDMetrics returns the Prometheus collectors for RAID health, sync, and member status.
func RAIDMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		raidHealthStatus,
		raidSyncInProgress,
		raidMemberCount,
		raidDegradedCount,
		raidSyncPercent,
	}
}

// updateRAIDMetrics sets all RAID gauges for a device class on a node.
func updateRAIDMetrics(nodeName, deviceClassName string, raidStatus *lvmv1alpha1.RAIDStatus) {
	if raidStatus == nil {
		raidHealthStatus.WithLabelValues(nodeName, deviceClassName).Set(0)
		raidSyncInProgress.WithLabelValues(nodeName, deviceClassName).Set(0)
		raidMemberCount.WithLabelValues(nodeName, deviceClassName).Set(0)
		raidDegradedCount.WithLabelValues(nodeName, deviceClassName).Set(0)
		raidSyncPercent.WithLabelValues(nodeName, deviceClassName).Set(100)
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

	raidMemberCount.WithLabelValues(nodeName, deviceClassName).Set(float64(raidStatus.MemberCount))
	raidDegradedCount.WithLabelValues(nodeName, deviceClassName).Set(float64(raidStatus.DegradedMemberCount))

	syncPct := float64(100)
	if raidStatus.MinSyncPercent != nil {
		syncPct = float64(*raidStatus.MinSyncPercent)
	}
	raidSyncPercent.WithLabelValues(nodeName, deviceClassName).Set(syncPct)
}

// deleteRAIDMetrics removes all RAID metric series for a device class on a node.
func deleteRAIDMetrics(nodeName, deviceClassName string) {
	raidHealthStatus.DeleteLabelValues(nodeName, deviceClassName)
	raidSyncInProgress.DeleteLabelValues(nodeName, deviceClassName)
	raidMemberCount.DeleteLabelValues(nodeName, deviceClassName)
	raidDegradedCount.DeleteLabelValues(nodeName, deviceClassName)
	raidSyncPercent.DeleteLabelValues(nodeName, deviceClassName)
}
