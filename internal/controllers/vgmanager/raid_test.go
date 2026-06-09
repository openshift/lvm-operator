package vgmanager

import (
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
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
