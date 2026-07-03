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

func TestUpdateRAIDMetrics(t *testing.T) {
	tests := []struct {
		name             string
		raidStatus       *lvmv1alpha1.RAIDStatus
		totalPVs         int
		missingPVs       int
		expectedHealth   float64
		expectedMembers  float64
		expectedDegraded float64
	}{
		{
			name: "healthy status with 4 PVs",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 100},
				},
			},
			totalPVs:         4,
			missingPVs:       0,
			expectedHealth:   0,
			expectedMembers:  4,
			expectedDegraded: 0,
		},
		{
			name: "degraded status with 1 missing PV",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 100, HealthStatus: "partial"},
				},
			},
			totalPVs:         4,
			missingPVs:       1,
			expectedHealth:   1,
			expectedMembers:  4,
			expectedDegraded: 1,
		},
		{
			name: "failed status",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusFailed,
			},
			totalPVs:         4,
			missingPVs:       2,
			expectedHealth:   2,
			expectedMembers:  4,
			expectedDegraded: 2,
		},
		{
			name:             "nil status clears to zero",
			raidStatus:       nil,
			totalPVs:         0,
			missingPVs:       0,
			expectedHealth:   0,
			expectedMembers:  0,
			expectedDegraded: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raidHealthStatus.Reset()
			raidMemberCount.Reset()
			raidDegradedCount.Reset()
			raidSyncPercent.Reset()

			updateRAIDMetrics("test-node", "test-dc", tt.raidStatus, tt.totalPVs, tt.missingPVs)

			if got := getGaugeValue(raidHealthStatus, "test-node", "test-dc"); got != tt.expectedHealth {
				t.Errorf("health: expected %f, got %f", tt.expectedHealth, got)
			}
			if got := getGaugeValue(raidMemberCount, "test-node", "test-dc"); got != tt.expectedMembers {
				t.Errorf("members: expected %f, got %f", tt.expectedMembers, got)
			}
			if got := getGaugeValue(raidDegradedCount, "test-node", "test-dc"); got != tt.expectedDegraded {
				t.Errorf("degraded: expected %f, got %f", tt.expectedDegraded, got)
			}
		})
	}
}

func TestUpdateRAIDMetricsSyncPercent(t *testing.T) {
	raidHealthStatus.Reset()
	raidMemberCount.Reset()
	raidDegradedCount.Reset()
	raidSyncPercent.Reset()

	updateRAIDMetrics("test-node", "test-dc", &lvmv1alpha1.RAIDStatus{
		Status: lvmv1alpha1.RAIDHealthStatusHealthy,
		LVHealth: []lvmv1alpha1.RAIDLVHealth{
			{Name: "lv1", SyncPercent: 42},
			{Name: "lv2", SyncPercent: 100},
		},
	}, 4, 0)

	if got := getGaugeValue(raidSyncPercent, "test-node", "test-dc", "lv1"); got != 42 {
		t.Errorf("sync percent lv1: expected 42, got %f", got)
	}
	if got := getGaugeValue(raidSyncPercent, "test-node", "test-dc", "lv2"); got != 100 {
		t.Errorf("sync percent lv2: expected 100, got %f", got)
	}
}

func TestDeleteRAIDMetrics(t *testing.T) {
	raidHealthStatus.Reset()
	raidMemberCount.Reset()
	raidDegradedCount.Reset()
	raidSyncPercent.Reset()

	updateRAIDMetrics("test-node", "test-dc", &lvmv1alpha1.RAIDStatus{
		Status: lvmv1alpha1.RAIDHealthStatusDegraded,
		LVHealth: []lvmv1alpha1.RAIDLVHealth{
			{Name: "lv1", SyncPercent: 50},
		},
	}, 4, 1)

	if n := collectMetricCount(raidHealthStatus); n != 1 {
		t.Fatalf("expected 1 health metric series, got %d", n)
	}

	deleteRAIDMetrics("test-node", "test-dc")

	if n := collectMetricCount(raidHealthStatus); n != 0 {
		t.Fatalf("expected 0 health metric series after delete, got %d", n)
	}
	if n := collectMetricCount(raidMemberCount); n != 0 {
		t.Fatalf("expected 0 member metric series after delete, got %d", n)
	}
	if n := collectMetricCount(raidDegradedCount); n != 0 {
		t.Fatalf("expected 0 degraded metric series after delete, got %d", n)
	}
	if n := collectMetricCount(raidSyncPercent); n != 0 {
		t.Fatalf("expected 0 sync metric series after delete, got %d", n)
	}
}
