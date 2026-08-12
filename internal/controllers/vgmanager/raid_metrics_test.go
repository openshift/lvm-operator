package vgmanager

import (
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func getGaugeValue(g *prometheus.GaugeVec, labels ...string) float64 {
	var m dto.Metric
	gauge, err := g.GetMetricWithLabelValues(labels...)
	if err != nil {
		return -1
	}
	if err := gauge.Write(&m); err != nil {
		return -1
	}
	return m.GetGauge().GetValue()
}

func TestUpdateRAIDMetrics(t *testing.T) {
	syncPct := func(v int) *int { return &v }

	tests := []struct {
		name                string
		raidStatus          *lvmv1alpha1.RAIDStatus
		expectedHealth      float64
		expectedSyncActive  float64
		expectedMembers     float64
		expectedDegraded    float64
		expectedSyncPercent float64
	}{
		{
			name: "healthy status with members",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 100},
				},
				MemberCount:    4,
				MinSyncPercent: syncPct(100),
			},
			expectedHealth:      0,
			expectedSyncActive:  0,
			expectedMembers:     4,
			expectedDegraded:    0,
			expectedSyncPercent: 100,
		},
		{
			name: "degraded status with missing member",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 100, HealthStatus: "partial"},
				},
				MemberCount:         4,
				DegradedMemberCount: 1,
				MinSyncPercent:      syncPct(100),
			},
			expectedHealth:      1,
			expectedSyncActive:  0,
			expectedMembers:     4,
			expectedDegraded:    1,
			expectedSyncPercent: 100,
		},
		{
			name: "failed status",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusFailed,
			},
			expectedHealth:      2,
			expectedSyncActive:  0,
			expectedMembers:     0,
			expectedDegraded:    0,
			expectedSyncPercent: 100,
		},
		{
			name: "sync in progress with percent",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 42},
				},
				MemberCount:    2,
				MinSyncPercent: syncPct(42),
			},
			expectedHealth:      0,
			expectedSyncActive:  1,
			expectedMembers:     2,
			expectedDegraded:    0,
			expectedSyncPercent: 42,
		},
		{
			name:                "nil status clears metrics to defaults",
			raidStatus:          nil,
			expectedHealth:      0,
			expectedSyncActive:  0,
			expectedMembers:     0,
			expectedDegraded:    0,
			expectedSyncPercent: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raidHealthStatus.Reset()
			raidSyncInProgress.Reset()
			raidMemberCount.Reset()
			raidDegradedCount.Reset()
			raidSyncPercent.Reset()

			updateRAIDMetrics("test-node", "test-dc", tt.raidStatus)

			gotHealth := getGaugeValue(raidHealthStatus, "test-node", "test-dc")
			if gotHealth != tt.expectedHealth {
				t.Errorf("health: expected %f, got %f", tt.expectedHealth, gotHealth)
			}
			gotSync := getGaugeValue(raidSyncInProgress, "test-node", "test-dc")
			if gotSync != tt.expectedSyncActive {
				t.Errorf("sync active: expected %f, got %f", tt.expectedSyncActive, gotSync)
			}
			gotMembers := getGaugeValue(raidMemberCount, "test-node", "test-dc")
			if gotMembers != tt.expectedMembers {
				t.Errorf("member count: expected %f, got %f", tt.expectedMembers, gotMembers)
			}
			gotDegraded := getGaugeValue(raidDegradedCount, "test-node", "test-dc")
			if gotDegraded != tt.expectedDegraded {
				t.Errorf("degraded count: expected %f, got %f", tt.expectedDegraded, gotDegraded)
			}
			gotSyncPct := getGaugeValue(raidSyncPercent, "test-node", "test-dc")
			if gotSyncPct != tt.expectedSyncPercent {
				t.Errorf("sync percent: expected %f, got %f", tt.expectedSyncPercent, gotSyncPct)
			}
		})
	}
}

func collectMetricCount(c prometheus.Collector) int {
	ch := make(chan prometheus.Metric, 100)
	c.Collect(ch)
	close(ch)
	count := 0
	for range ch {
		count++
	}
	return count
}

func TestDeleteRAIDMetrics(t *testing.T) {
	raidHealthStatus.Reset()
	raidSyncInProgress.Reset()
	raidMemberCount.Reset()
	raidDegradedCount.Reset()
	raidSyncPercent.Reset()

	updateRAIDMetrics("test-node", "test-dc", &lvmv1alpha1.RAIDStatus{
		Status:      lvmv1alpha1.RAIDHealthStatusDegraded,
		MemberCount: 4,
	})

	if n := collectMetricCount(raidHealthStatus); n != 1 {
		t.Fatalf("expected 1 metric series, got %d", n)
	}

	deleteRAIDMetrics("test-node", "test-dc")

	for name, collector := range map[string]prometheus.Collector{
		"raidHealthStatus":   raidHealthStatus,
		"raidSyncInProgress": raidSyncInProgress,
		"raidMemberCount":    raidMemberCount,
		"raidDegradedCount":  raidDegradedCount,
		"raidSyncPercent":    raidSyncPercent,
	} {
		if n := collectMetricCount(collector); n != 0 {
			t.Fatalf("expected 0 %s series after delete, got %d", name, n)
		}
	}
}
