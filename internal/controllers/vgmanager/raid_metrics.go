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
			Help: "Number of missing or failed physical volumes in the RAID volume group.",
		},
		[]string{"node", "device_class"},
	)

	raidSyncPercent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lvms_raid_sync_percent",
			Help: "RAID resynchronization progress per logical volume (0-100).",
		},
		[]string{"node", "device_class", "lv"},
	)
)

func RAIDMetrics() []prometheus.Collector {
	return []prometheus.Collector{raidHealthStatus, raidMemberCount, raidDegradedCount, raidSyncPercent}
}

func updateRAIDMetrics(nodeName, deviceClassName string, raidStatus *lvmv1alpha1.RAIDStatus, totalPVs, missingPVs int) {
	raidMemberCount.WithLabelValues(nodeName, deviceClassName).Set(float64(totalPVs))
	raidDegradedCount.WithLabelValues(nodeName, deviceClassName).Set(float64(missingPVs))

	if raidStatus == nil {
		raidHealthStatus.WithLabelValues(nodeName, deviceClassName).Set(0)
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

	raidSyncPercent.DeletePartialMatch(prometheus.Labels{"node": nodeName, "device_class": deviceClassName})
	for _, lv := range raidStatus.LVHealth {
		raidSyncPercent.WithLabelValues(nodeName, deviceClassName, lv.Name).Set(float64(lv.SyncPercent))
	}
}

func deleteRAIDMetrics(nodeName, deviceClassName string) {
	raidHealthStatus.DeleteLabelValues(nodeName, deviceClassName)
	raidMemberCount.DeleteLabelValues(nodeName, deviceClassName)
	raidDegradedCount.DeleteLabelValues(nodeName, deviceClassName)
	raidSyncPercent.DeletePartialMatch(prometheus.Labels{"node": nodeName, "device_class": deviceClassName})
}
