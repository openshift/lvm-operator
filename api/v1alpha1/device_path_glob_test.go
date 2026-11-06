/*
Copyright © 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// Two patterns are normally left to the node: ownership picks the winner and the loser
// merely goes without the disk. Force wipe destroys the disk before any ownership
// exists, so admission has to settle that pair itself.
func TestGlobOverlapUnderForceWipe(t *testing.T) {
	wipe := func(paths ...DevicePath) *DeviceSelector {
		return &DeviceSelector{Paths: paths, ForceWipeDevicesAndDestroyAllData: ptr.To(true)}
	}
	plain := func(paths ...DevicePath) *DeviceSelector { return &DeviceSelector{Paths: paths} }

	for _, tt := range []struct {
		name      string
		vg1, vg2  *DeviceSelector
		wantError bool
	}{
		{
			name: "a subset pattern is rejected when the other class force wipes",
			vg1:  wipe("glob:/dev/nvme*"), vg2: plain("glob:/dev/nvme2*"), wantError: true,
		},
		{
			name: "the same pair without force wipe is still left to the node",
			vg1:  plain("glob:/dev/nvme*"), vg2: plain("glob:/dev/nvme2*"),
		},
		{
			name: "patterns with diverging prefixes stay accepted under force wipe",
			vg1:  wipe("glob:/dev/sd*"), vg2: plain("glob:/dev/nvme*"),
		},
		{
			name: "a pattern in another directory cannot collide",
			vg1:  wipe("glob:/dev/disk/by-id/*"), vg2: plain("glob:/dev/sd*"),
		},
		{
			name: "a wildcard that could match anything is rejected",
			vg1:  wipe("glob:/dev/sd*"), vg2: plain("glob:/dev/*1"), wantError: true,
		},
		{
			// Segment counts decide disjointedness, so a stray separator must not make
			// two colliding patterns look like they sit in different directories.
			name: "a non-canonical pattern still collides",
			vg1:  wipe("glob:/dev//sd*"), vg2: plain("glob:/dev/sdb*"), wantError: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
				{Name: "vg1", DeviceSelector: tt.vg1},
				{Name: "vg2", DeviceSelector: tt.vg2},
			}}}}
			err := (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster)
			if tt.wantError {
				require.ErrorContains(t, err, "overlaps")
			} else {
				require.NoError(t, err)
			}
		})
	}

	// Within one class an overlap is harmless for wiping: the same volume group wipes
	// the disk once. Keep the weaker rule so force wipe does not reject more than before.
	same := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
		{Name: "vg1", DeviceSelector: wipe("glob:/dev/nvme*", "glob:/dev/nvme2*")},
	}}}}
	require.NoError(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(same))
}

func TestDevicePathGlob(t *testing.T) {
	for _, tt := range []struct {
		path    DevicePath
		device  string
		matches bool
	}{
		// The case that forced globs over regular expressions: as a regexp, "/dev/nvme2*"
		// means "/dev/nvme" followed by any number of "2"s and silently selects nothing.
		{"glob:/dev/nvme2*", "/dev/nvme2n1", true},
		{"glob:/dev/nvme2*", "/dev/nvme2n1p1", true},
		{"glob:/dev/nvme2*", "/dev/nvme3n1", false},
		{"glob:/dev/sd[b-d]", "/dev/sdc", true},
		{"glob:/dev/sd[b-d]", "/dev/sda", false},
		{"glob:/dev/sd[b-d]", "/dev/sdb1", false},
		{"glob:/dev/nvme?n1", "/dev/nvme0n1", true},
		{"glob:/dev/nvme?n1", "/dev/nvme12n1", false},
		// "." is a literal dot, not "any character".
		{"glob:/dev/sd.", "/dev/sda", false},
		{"glob:/dev/sd.", "/dev/sd.", true},
		// "*" never crosses a "/", so a pattern cannot escape its directory.
		{"glob:/dev/*", "/dev/sda", true},
		{"glob:/dev/*", "/dev/disk/by-id/wwn-0x1", false},
		{"glob:/dev/sdb", "/prefix/dev/sdb", false},
	} {
		t.Run(string(tt.path)+tt.device, func(t *testing.T) {
			require.True(t, tt.path.IsGlob())
			matched, err := tt.path.Match(tt.device)
			require.NoError(t, err)
			assert.Equal(t, tt.matches, matched)
		})
	}

	assert.False(t, DevicePath("/dev/disk/by-id/a[b]").IsGlob())
	assert.NoError(t, DevicePath("/dev/disk/by-id/a[b]").Validate())
	require.ErrorContains(t, DevicePath("glob:/dev/[").Validate(), "invalid device path pattern")
}

func TestGlobPathValidation(t *testing.T) {
	validator := &lvmClusterValidator{}
	for _, optional := range []bool{false, true} {
		for _, tt := range []struct {
			path      DevicePath
			wantError string
		}{
			{"glob:/dev/sd[b-d]", ""},
			{"glob:/dev/nvme2*", ""},
			{"/dev/disk/by-id/disk[0]", ""},
			{"glob:/dev/[", "invalid device path pattern"},
			{"glob:dev/sda", "must be an absolute path"},
			{"glob:", "must be an absolute path"},
			// Traversal was only checked for patterns, so a literal could walk out of
			// /dev unnoticed. Clean now runs for both.
			{"/dev/../etc/shadow", "must be an absolute path"},
			{"glob:/dev/../etc/*", "must be an absolute path"},
			{"/dev/disk/../disk/by-id/wwn-0x5", ""},
			// A non-canonical pattern is not merely untidy: filepath.Match never
			// collapses the extra separator, so it silently selects nothing.
			{"glob:/dev//sd*", "never match any device"},
			{"glob:/dev/./sd*", "never match any device"},
			{"glob:/dev/disk/../sd*", "never match any device"},
		} {
			t.Run(string(tt.path)+map[bool]string{true: " optional", false: " required"}[optional], func(t *testing.T) {
				selector := &DeviceSelector{}
				if optional {
					selector.OptionalPaths = []DevicePath{tt.path}
				} else {
					selector.Paths = []DevicePath{tt.path}
				}
				cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{{Name: "vg1", DeviceSelector: selector}}}}}
				err := validator.verifyAbsolutePath(cluster)
				if tt.wantError == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tt.wantError)
				}
			})
		}
	}
}

func TestGlobOverlapBetweenDeviceClasses(t *testing.T) {
	for _, tt := range []struct {
		name      string
		vg1, vg2  *DeviceSelector
		wantError bool
	}{
		{
			name: "a pattern overlapping a literal path in another class is rejected",
			vg1:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}},
			vg2:  &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}, wantError: true,
		},
		{
			name: "the same applies to optional paths",
			vg1:  &DeviceSelector{OptionalPaths: []DevicePath{"glob:/dev/sd*"}},
			vg2:  &DeviceSelector{OptionalPaths: []DevicePath{"/dev/sdb"}}, wantError: true,
		},
		{
			name: "the same pattern in two classes is rejected",
			vg1:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}},
			vg2:  &DeviceSelector{OptionalPaths: []DevicePath{"glob:/dev/sd*"}}, wantError: true,
		},
		{
			name: "a pattern that cannot match the other class's literal is accepted",
			vg1:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}},
			vg2:  &DeviceSelector{Paths: []DevicePath{"/dev/nvme0n1"}},
		},
		{
			// Admission cannot know which of these a node actually has, so it admits them
			// and VG Manager decides against real devices. Here every /dev/sdb* device is
			// also a /dev/sd* device, so they do collide on any node with one.
			name: "a pattern that is a subset of another is left to the node",
			vg1:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}},
			vg2:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sdb*"}},
		},
		{
			// Neither pattern matches the other's text, but both match /dev/sda1.
			name: "patterns colliding only on a concrete name are left to the node",
			vg1:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}},
			vg2:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/*1"}},
		},
		{
			name: "disjoint character classes are accepted",
			vg1:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd[a-c]"}},
			vg2:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd[d-f]"}},
		},
		{
			// A literal spelled with a traversal still names /dev/sda, so a pattern
			// over /dev/sd* claims the same disk. Comparing the raw text missed it.
			name: "a non-canonical literal is matched against a pattern",
			vg1:  &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}},
			vg2:  &DeviceSelector{Paths: []DevicePath{"/dev/disk/../sda"}}, wantError: true,
		},
		{
			name: "two literals naming one disk in different spellings collide",
			vg1:  &DeviceSelector{Paths: []DevicePath{"/dev/sda"}},
			vg2:  &DeviceSelector{Paths: []DevicePath{"/dev/disk/../sda"}}, wantError: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
				{Name: "vg1", DeviceSelector: tt.vg1},
				{Name: "vg2", DeviceSelector: tt.vg2},
			}}}}
			err := (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster)
			if tt.wantError {
				require.ErrorContains(t, err, "overlaps")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Overlapping entries inside one device class are a configuration mistake, the same way
// a literal path repeated twice always has been.
func TestGlobOverlapWithinDeviceClass(t *testing.T) {
	for _, tt := range []struct {
		name     string
		selector *DeviceSelector
	}{
		{"a pattern and a literal it matches", &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*", "/dev/sdb"}}},
		{"the same pattern twice", &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*", "glob:/dev/sd*"}}},
		{"required and optional overlapping", &DeviceSelector{
			Paths:         []DevicePath{"glob:/dev/sd*"},
			OptionalPaths: []DevicePath{"/dev/sdb"},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
				{Name: "vg1", DeviceSelector: tt.selector},
			}}}}
			require.ErrorContains(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster), "multiple places")
		})
	}
}

func TestGlobNoOverlapWithinDeviceClass(t *testing.T) {
	cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
		{Name: "vg1", DeviceSelector: &DeviceSelector{
			Paths:         []DevicePath{"glob:/dev/sd[a-c]", "/dev/nvme0n1"},
			OptionalPaths: []DevicePath{"glob:/dev/sd[d-f]"},
		}},
	}}}}
	require.NoError(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster))

	// Two different patterns in one class are admitted for the same reason they are
	// across classes: only a node knows whether any disk satisfies both.
	cluster.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []DevicePath{"glob:/dev/sd*", "glob:/dev/sd[b-c]"}
	require.NoError(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster))
}

// Overlap is only a conflict when the classes can land on the same node.
func TestGlobOverlapRespectsNodeSelector(t *testing.T) {
	nodeSelector := func(value string) *corev1.NodeSelector { return nodeSelectorIn("host", value) }
	cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
		{Name: "vg1", NodeSelector: nodeSelector("a"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}}},
		{Name: "vg2", NodeSelector: nodeSelector("b"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
	}}}}
	require.NoError(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster))
}

// RAID needs an exact device count that a pattern cannot supply at admission, and RAID
// geometry is fixed at lvcreate time, so a device discovered later could not join the
// array anyway. Rejecting the combination now keeps the option of allowing it later.
func TestGlobRejectedWithRAID(t *testing.T) {
	for _, optional := range []bool{false, true} {
		for _, raidType := range []RAIDType{RAIDTypeRAID1, RAIDTypeRAID5, RAIDTypeRAID10} {
			selector := &DeviceSelector{Paths: []DevicePath{"/dev/sda", "/dev/sdb", "/dev/sdc", "/dev/sdd"}}
			if optional {
				selector.OptionalPaths = []DevicePath{"glob:/dev/nvme[0-3]n1"}
			} else {
				selector.Paths = append(selector.Paths, "glob:/dev/nvme[0-3]n1")
			}
			cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
				{Name: "vg1", DeviceSelector: selector, RAIDConfig: &RAIDConfig{Type: raidType}},
			}}}}
			require.ErrorIs(t, (&lvmClusterValidator{}).verifyRAIDConfig(cluster), ErrRAIDGlobPathsUnsupported)
		}
	}
}

func TestLiteralPathsStillAllowedWithRAID(t *testing.T) {
	cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
		{
			Name:           "vg1",
			DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sda", "/dev/sdb", "/dev/sdc", "/dev/sdd"}},
			RAIDConfig:     &RAIDConfig{Type: RAIDTypeRAID5},
		},
	}}}}
	require.NoError(t, (&lvmClusterValidator{}).verifyRAIDConfig(cluster))
}

// A device class without a nodeSelector runs on every node, so it must be compared
// against classes that do have one.
func TestGlobOverlapWithUnselectedDeviceClass(t *testing.T) {
	nodeSelector := &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
		MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "host", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}}},
	}}}
	cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
		{Name: "everywhere", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}}},
		{Name: "vg2", NodeSelector: nodeSelector, DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
	}}}}
	require.ErrorContains(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster), "overlaps")

	cluster.Spec.Storage.DeviceClasses[1].DeviceSelector.Paths = []DevicePath{"/dev/nvme0n1"}
	require.NoError(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster))
}

// Different node selectors only skip the overlap check when they provably cannot share
// a node. "host in (a)" and "zone in (z1)" can both select one node, so they are checked.
func TestGlobOverlapAcrossUnprovablyDisjointNodeSelectors(t *testing.T) {
	cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{
		{Name: "vg1", NodeSelector: nodeSelectorIn("host", "a"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}}},
		{Name: "vg2", NodeSelector: nodeSelectorIn("zone", "z1"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
	}}}}
	require.ErrorContains(t, (&lvmClusterValidator{}).verifyNoDeviceOverlap(cluster), "overlaps")
}

func nodeSelectorIn(key string, values ...string) *corev1.NodeSelector {
	return &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
		MatchExpressions: []corev1.NodeSelectorRequirement{{Key: key, Operator: corev1.NodeSelectorOpIn, Values: values}},
	}}}
}
