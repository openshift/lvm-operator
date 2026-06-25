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
	tests := []struct {
		name               string
		raidStatus         *lvmv1alpha1.RAIDStatus
		expectedHealth     float64
		expectedSyncActive float64
	}{
		{
			name: "healthy status",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 100},
				},
			},
			expectedHealth:     0,
			expectedSyncActive: 0,
		},
		{
			name: "degraded status",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 100, HealthStatus: "partial"},
				},
			},
			expectedHealth:     1,
			expectedSyncActive: 0,
		},
		{
			name: "failed status",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusFailed,
			},
			expectedHealth:     2,
			expectedSyncActive: 0,
		},
		{
			name: "sync in progress",
			raidStatus: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv1", SyncPercent: 42},
				},
			},
			expectedHealth:     0,
			expectedSyncActive: 1,
		},
		{
			name:               "nil status clears metrics to zero",
			raidStatus:         nil,
			expectedHealth:     0,
			expectedSyncActive: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raidHealthStatus.Reset()
			raidSyncInProgress.Reset()

			updateRAIDMetrics("test-node", "test-dc", tt.raidStatus)

			gotHealth := getGaugeValue(raidHealthStatus, "test-node", "test-dc")
			if gotHealth != tt.expectedHealth {
				t.Errorf("health: expected %f, got %f", tt.expectedHealth, gotHealth)
			}
			gotSync := getGaugeValue(raidSyncInProgress, "test-node", "test-dc")
			if gotSync != tt.expectedSyncActive {
				t.Errorf("sync: expected %f, got %f", tt.expectedSyncActive, gotSync)
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

	updateRAIDMetrics("test-node", "test-dc", &lvmv1alpha1.RAIDStatus{
		Status: lvmv1alpha1.RAIDHealthStatusDegraded,
	})

	if n := collectMetricCount(raidHealthStatus); n != 1 {
		t.Fatalf("expected 1 metric series, got %d", n)
	}

	deleteRAIDMetrics("test-node", "test-dc")

	if n := collectMetricCount(raidHealthStatus); n != 0 {
		t.Fatalf("expected 0 metric series after delete, got %d", n)
	}
	if n := collectMetricCount(raidSyncInProgress); n != 0 {
		t.Fatalf("expected 0 sync metric series after delete, got %d", n)
	}
}
