package vgmanager

import (
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func TestBuildRAIDLVCreateOptions(t *testing.T) {
	tests := []struct {
		name     string
		config   *lvmv1alpha1.RAIDConfig
		expected []string
	}{
		{
			name: "raid1 default mirrors",
			config: &lvmv1alpha1.RAIDConfig{
				Type: lvmv1alpha1.RAIDTypeRAID1,
			},
			expected: []string{"--type", "raid1", "-m", "1"},
		},
		{
			name: "raid1 with mirrors=2",
			config: &lvmv1alpha1.RAIDConfig{
				Type:    lvmv1alpha1.RAIDTypeRAID1,
				Mirrors: ptr.To(2),
			},
			expected: []string{"--type", "raid1", "-m", "2"},
		},
		{
			name: "raid5 no optional fields",
			config: &lvmv1alpha1.RAIDConfig{
				Type: lvmv1alpha1.RAIDTypeRAID5,
			},
			expected: []string{"--type", "raid5"},
		},
		{
			name: "raid5 with stripes and stripeSize",
			config: &lvmv1alpha1.RAIDConfig{
				Type:       lvmv1alpha1.RAIDTypeRAID5,
				Stripes:    ptr.To(3),
				StripeSize: ptr.To(resource.MustParse("256Ki")),
			},
			expected: []string{"--type", "raid5", "--stripes", "3", "--stripesize", "256k"},
		},
		{
			name: "raid4 with stripeSize only",
			config: &lvmv1alpha1.RAIDConfig{
				Type:       lvmv1alpha1.RAIDTypeRAID4,
				StripeSize: ptr.To(resource.MustParse("128Ki")),
			},
			expected: []string{"--type", "raid4", "--stripesize", "128k"},
		},
		{
			name: "raid6 with stripes",
			config: &lvmv1alpha1.RAIDConfig{
				Type:    lvmv1alpha1.RAIDTypeRAID6,
				Stripes: ptr.To(4),
			},
			expected: []string{"--type", "raid6", "--stripes", "4"},
		},
		{
			name: "raid10 with mirrors and stripes",
			config: &lvmv1alpha1.RAIDConfig{
				Type:    lvmv1alpha1.RAIDTypeRAID10,
				Mirrors: ptr.To(1),
				Stripes: ptr.To(2),
			},
			expected: []string{"--type", "raid10", "-m", "1", "--stripes", "2"},
		},
		{
			name: "raid10 with all fields",
			config: &lvmv1alpha1.RAIDConfig{
				Type:       lvmv1alpha1.RAIDTypeRAID10,
				Mirrors:    ptr.To(1),
				Stripes:    ptr.To(3),
				StripeSize: ptr.To(resource.MustParse("64Ki")),
			},
			expected: []string{"--type", "raid10", "-m", "1", "--stripes", "3", "--stripesize", "64k"},
		},
		{
			name: "raid10 default mirrors no stripes",
			config: &lvmv1alpha1.RAIDConfig{
				Type: lvmv1alpha1.RAIDTypeRAID10,
			},
			expected: []string{"--type", "raid10", "-m", "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRAIDLVCreateOptions(tt.config)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("option[%d]: expected %q, got %q", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func TestValidateRAIDDeviceCount(t *testing.T) {
	tests := []struct {
		name        string
		config      *lvmv1alpha1.RAIDConfig
		deviceCount int
		expectError bool
	}{
		{
			name:        "raid1 with 2 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1},
			deviceCount: 2,
		},
		{
			name:        "raid1 with 1 device fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1},
			deviceCount: 1,
			expectError: true,
		},
		{
			name:        "raid1 mirrors=2 with 3 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1, Mirrors: ptr.To(2)},
			deviceCount: 3,
		},
		{
			name:        "raid1 mirrors=2 with 2 devices fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1, Mirrors: ptr.To(2)},
			deviceCount: 2,
			expectError: true,
		},
		{
			name:        "raid5 with 3 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID5},
			deviceCount: 3,
		},
		{
			name:        "raid5 with 2 devices fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID5},
			deviceCount: 2,
			expectError: true,
		},
		{
			name:        "raid5 stripes=4 with 5 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID5, Stripes: ptr.To(4)},
			deviceCount: 5,
		},
		{
			name:        "raid5 stripes=4 with 4 devices fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID5, Stripes: ptr.To(4)},
			deviceCount: 4,
			expectError: true,
		},
		{
			name:        "raid6 with 5 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID6},
			deviceCount: 5,
		},
		{
			name:        "raid6 with 4 devices fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID6},
			deviceCount: 4,
			expectError: true,
		},
		{
			name:        "raid10 with 4 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10},
			deviceCount: 4,
		},
		{
			name:        "raid10 with 3 devices fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10},
			deviceCount: 3,
			expectError: true,
		},
		{
			name:        "raid10 mirrors=1 odd count fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10},
			deviceCount: 5,
			expectError: true,
		},
		{
			name:        "raid10 mirrors=1 even count",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10},
			deviceCount: 6,
		},
		{
			name:        "raid10 mirrors=2 count divisible by 3",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10, Mirrors: ptr.To(2)},
			deviceCount: 6,
		},
		{
			name:        "raid10 mirrors=2 count not divisible by 3 fails",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10, Mirrors: ptr.To(2)},
			deviceCount: 7,
			expectError: true,
		},
		{
			name:        "raid4 with 3 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID4},
			deviceCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRAIDDeviceCount(tt.config, tt.deviceCount)
			if tt.expectError && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
		})
	}
}

func TestComputeOverheadFactor(t *testing.T) {
	tests := []struct {
		name        string
		config      *lvmv1alpha1.RAIDConfig
		deviceCount int
		expected    float64
	}{
		{
			name:        "raid1 default mirrors",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1},
			deviceCount: 2,
			expected:    2.0,
		},
		{
			name:        "raid1 mirrors=2",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID1, Mirrors: ptr.To(2)},
			deviceCount: 3,
			expected:    3.0,
		},
		{
			name:        "raid5 stripes specified",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID5, Stripes: ptr.To(3)},
			deviceCount: 4,
			expected:    4.0 / 3.0,
		},
		{
			name:        "raid5 stripes not specified 4 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID5},
			deviceCount: 4,
			expected:    4.0 / 3.0,
		},
		{
			name:        "raid4 stripes specified",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID4, Stripes: ptr.To(3)},
			deviceCount: 4,
			expected:    4.0 / 3.0,
		},
		{
			name:        "raid4 stripes not specified 5 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID4},
			deviceCount: 5,
			expected:    5.0 / 4.0,
		},
		{
			name:        "raid6 stripes specified",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID6, Stripes: ptr.To(4)},
			deviceCount: 6,
			expected:    6.0 / 4.0,
		},
		{
			name:        "raid6 stripes not specified 5 devices",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID6},
			deviceCount: 5,
			expected:    5.0 / 3.0,
		},
		{
			name:        "raid10 default mirrors",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10},
			deviceCount: 4,
			expected:    2.0,
		},
		{
			name:        "raid10 mirrors=2",
			config:      &lvmv1alpha1.RAIDConfig{Type: lvmv1alpha1.RAIDTypeRAID10, Mirrors: ptr.To(2)},
			deviceCount: 6,
			expected:    3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeOverheadFactor(tt.config, tt.deviceCount)
			if got != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, got)
			}
		})
	}
}

func TestBuildRAIDStatus(t *testing.T) {
	tests := []struct {
		name                   string
		lvs                    []lvm.LogicalVolume
		pvs                    []lvm.PhysicalVolume
		raidType               lvmv1alpha1.RAIDType
		expected               *lvmv1alpha1.RAIDStatus
		expectedMemberCount    int
		expectedDegradedCount  int
		expectedMinSyncPercent *int
	}{
		{
			name:     "no RAID LVs and no PVs returns nil",
			lvs:      []lvm.LogicalVolume{{Name: "thin-pool", LvAttr: "twi-a-t---"}},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: nil,
		},
		{
			name: "all healthy with PVs",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
				{Name: "lv-pvc-abc_rimage_0", LvAttr: "iwi-aor---", RAIDSyncPercent: "", LVLayout: "linear"},
				{Name: "lv-pvc-abc_rimage_1", LvAttr: "iwi-aor---", RAIDSyncPercent: "", LVLayout: "linear"},
				{Name: "lv-pvc-abc_rmeta_0", LvAttr: "ewi-aor---", RAIDSyncPercent: "", LVLayout: "linear"},
				{Name: "lv-pvc-abc_rmeta_1", LvAttr: "ewi-aor---", RAIDSyncPercent: "", LVLayout: "linear"},
			},
			pvs: []lvm.PhysicalVolume{
				{PvName: "/dev/sda"},
				{PvName: "/dev/sdb"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100},
				},
			},
			expectedMemberCount:    2,
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "syncing LV",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "42.50", LVHealthStatus: "", LVLayout: "raid,raid1"},
			},
			pvs: []lvm.PhysicalVolume{
				{PvName: "/dev/sda"},
				{PvName: "/dev/sdb"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 42},
				},
			},
			expectedMemberCount:    2,
			expectedMinSyncPercent: ptr.To(42),
		},
		{
			name: "single LV with partial health is degraded",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r--p", RAIDSyncPercent: "100.00", LVHealthStatus: "partial", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100, HealthStatus: "partial"},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "multiple LVs mixed health",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
				{Name: "lv-pvc-def", LvAttr: "rwi-a-r--p", RAIDSyncPercent: "100.00", LVHealthStatus: "partial", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100},
					{Name: "lv-pvc-def", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100, HealthStatus: "partial"},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "all LVs partial is degraded not failed",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r--p", RAIDSyncPercent: "100.00", LVHealthStatus: "partial", LVLayout: "raid,raid1"},
				{Name: "lv-pvc-def", LvAttr: "rwi-a-r--p", RAIDSyncPercent: "100.00", LVHealthStatus: "partial", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100, HealthStatus: "partial"},
					{Name: "lv-pvc-def", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100, HealthStatus: "partial"},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "all LVs with non-partial health status is failed",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "refresh needed", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusFailed,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100, HealthStatus: "refresh needed"},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "mixed failed and partial is degraded not failed",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "refresh needed", LVLayout: "raid,raid1"},
				{Name: "lv-pvc-def", LvAttr: "rwi-a-r--p", RAIDSyncPercent: "100.00", LVHealthStatus: "partial", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100, HealthStatus: "refresh needed"},
					{Name: "lv-pvc-def", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100, HealthStatus: "partial"},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "RAID no initial sync volume type R",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "Rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "raid5 LV",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "", LVLayout: "raid,raid5"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID5,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID5, SyncPercent: 100},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "partial flag without health status triggers degraded",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r-p-", RAIDSyncPercent: "100.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "non-RAID LVs only and no PVs",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-linear", LvAttr: "-wi-a-----", RAIDSyncPercent: "", LVHealthStatus: "", LVLayout: "linear"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: nil,
		},
		{
			name: "empty sync percent defaults to 100",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "", LVHealthStatus: "", LVLayout: "raid,raid1"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100},
				},
			},
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "PVs with one missing escalates to degraded",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
			},
			pvs: []lvm.PhysicalVolume{
				{PvName: "/dev/sda"},
				{PvName: "/dev/sdb", PvMissing: "missing"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusDegraded,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100},
				},
			},
			expectedMemberCount:    2,
			expectedDegradedCount:  1,
			expectedMinSyncPercent: ptr.To(100),
		},
		{
			name: "no RAID LVs but PVs exist returns status with member counts",
			pvs: []lvm.PhysicalVolume{
				{PvName: "/dev/sda"},
				{PvName: "/dev/sdb"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
			},
			expectedMemberCount: 2,
		},
		{
			name: "multiple LVs with different sync percents picks minimum",
			lvs: []lvm.LogicalVolume{
				{Name: "lv-pvc-abc", LvAttr: "rwi-a-r---", RAIDSyncPercent: "100.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
				{Name: "lv-pvc-def", LvAttr: "rwi-a-r---", RAIDSyncPercent: "55.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
				{Name: "lv-pvc-ghi", LvAttr: "rwi-a-r---", RAIDSyncPercent: "78.00", LVHealthStatus: "", LVLayout: "raid,raid1"},
			},
			pvs: []lvm.PhysicalVolume{
				{PvName: "/dev/sda"},
				{PvName: "/dev/sdb"},
			},
			raidType: lvmv1alpha1.RAIDTypeRAID1,
			expected: &lvmv1alpha1.RAIDStatus{
				Status: lvmv1alpha1.RAIDHealthStatusHealthy,
				LVHealth: []lvmv1alpha1.RAIDLVHealth{
					{Name: "lv-pvc-abc", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 100},
					{Name: "lv-pvc-def", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 55},
					{Name: "lv-pvc-ghi", RAIDType: lvmv1alpha1.RAIDTypeRAID1, SyncPercent: 78},
				},
			},
			expectedMemberCount:    2,
			expectedMinSyncPercent: ptr.To(55),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRAIDStatus(tt.lvs, tt.pvs, tt.raidType)
			if tt.expected == nil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %+v, got nil", tt.expected)
			}
			if got.Status != tt.expected.Status {
				t.Errorf("status: expected %q, got %q", tt.expected.Status, got.Status)
			}
			if len(got.LVHealth) != len(tt.expected.LVHealth) {
				t.Fatalf("lvHealth length: expected %d, got %d", len(tt.expected.LVHealth), len(got.LVHealth))
			}
			for i := range got.LVHealth {
				if got.LVHealth[i] != tt.expected.LVHealth[i] {
					t.Errorf("lvHealth[%d]: expected %+v, got %+v", i, tt.expected.LVHealth[i], got.LVHealth[i])
				}
			}
			if got.MemberCount != tt.expectedMemberCount {
				t.Errorf("memberCount: expected %d, got %d", tt.expectedMemberCount, got.MemberCount)
			}
			if got.DegradedMemberCount != tt.expectedDegradedCount {
				t.Errorf("degradedMemberCount: expected %d, got %d", tt.expectedDegradedCount, got.DegradedMemberCount)
			}
			if tt.expectedMinSyncPercent == nil {
				if got.MinSyncPercent != nil {
					t.Errorf("minSyncPercent: expected nil, got %d", *got.MinSyncPercent)
				}
			} else {
				if got.MinSyncPercent == nil {
					t.Errorf("minSyncPercent: expected %d, got nil", *tt.expectedMinSyncPercent)
				} else if *got.MinSyncPercent != *tt.expectedMinSyncPercent {
					t.Errorf("minSyncPercent: expected %d, got %d", *tt.expectedMinSyncPercent, *got.MinSyncPercent)
				}
			}
		})
	}
}
