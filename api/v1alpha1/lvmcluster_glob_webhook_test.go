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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func roleSelector(role string) *corev1.NodeSelector {
	return &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
		MatchExpressions: []corev1.NodeSelectorRequirement{{Key: role, Operator: corev1.NodeSelectorOpExists}},
	}}}
}

func TestVerifyNoDeviceOverlap(t *testing.T) {
	const worker, infra = "node-role.kubernetes.io/worker", "node-role.kubernetes.io/infra"

	for _, tt := range []struct {
		name      string
		classes   []DeviceClass
		wantError string
	}{
		// Exists does not prove selectors disjoint, but releases before patterns accepted this,
		// so rejecting it now would freeze updates on existing clusters.
		{
			name: "literal path shared by role selectors stays accepted",
			classes: []DeviceClass{
				{Name: "vg1", NodeSelector: roleSelector(worker), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
				{Name: "vg2", NodeSelector: roleSelector(infra), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
			},
		},
		{
			name: "pattern against the same role selectors is rejected",
			classes: []DeviceClass{
				{Name: "vg1", NodeSelector: roleSelector(worker), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}}},
				{Name: "vg2", NodeSelector: roleSelector(infra), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
			},
			wantError: "overlaps in two different deviceClasss",
		},
		{
			name: "pattern on provably disjoint nodes is accepted",
			classes: []DeviceClass{
				{Name: "vg1", NodeSelector: zoneSelector("z1"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}}},
				{Name: "vg2", NodeSelector: zoneSelector("z2"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}}},
			},
		},
		{
			name: "identical node selectors still catch a shared literal path",
			classes: []DeviceClass{
				{Name: "vg1", NodeSelector: roleSelector(worker), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
				{Name: "vg2", NodeSelector: roleSelector(worker), DeviceSelector: &DeviceSelector{OptionalPaths: []DevicePath{"/dev/sdb"}}},
			},
			wantError: "overlaps in two different deviceClasss",
		},
		{
			name: "a path repeated inside one device class is still rejected",
			classes: []DeviceClass{
				{Name: "vg1", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}, OptionalPaths: []DevicePath{"/dev/sdb"}}},
			},
			wantError: "specified at multiple places",
		},
		// Single-node clusters run every device class on the one node, so two classes
		// without a node selector always collide.
		{
			name: "no node selector on either class is rejected",
			classes: []DeviceClass{
				{Name: "vg1", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
				{Name: "vg2", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
			},
			wantError: "overlaps in two different deviceClasss",
		},
		// An unset node selector means every node, so it reaches the other class's nodes.
		// A pattern says so; two literals keep the pre-pattern blind spot.
		{
			name: "unset against a set node selector is caught only for a pattern",
			classes: []DeviceClass{
				{Name: "vg1", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}}},
				{Name: "vg2", NodeSelector: zoneSelector("z1"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
			},
			wantError: "overlaps in two different deviceClasss",
		},
		{
			name: "unset against a set node selector is missed for two literals",
			classes: []DeviceClass{
				{Name: "vg1", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
				{Name: "vg2", NodeSelector: zoneSelector("z1"), DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
			},
		},
		// Non-overlapping paths are never asked the node question at all.
		{
			name: "distinct paths on one node are fine",
			classes: []DeviceClass{
				{Name: "vg1", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdb"}}},
				{Name: "vg2", DeviceSelector: &DeviceSelector{Paths: []DevicePath{"/dev/sdc"}}},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: tt.classes}}}
			err := (&lvmClusterValidator{}).verifyNoDeviceOverlap(l)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
		})
	}
}

func TestAllowGlobForceWipe(t *testing.T) {
	for _, optional := range []bool{false, true} {
		selector := &DeviceSelector{Paths: []DevicePath{"glob:/dev/sd*"}, ForceWipeDevicesAndDestroyAllData: ptr.To(true)}
		if optional {
			selector.OptionalPaths, selector.Paths = selector.Paths, nil
		}
		cluster := &LVMCluster{Spec: LVMClusterSpec{Storage: Storage{DeviceClasses: []DeviceClass{{Name: "vg1", DeviceSelector: selector}}}}}
		require.NoError(t, (&lvmClusterValidator{}).verifyAbsolutePath(cluster))
		selector.ForceWipeDevicesAndDestroyAllData = ptr.To(false)
		require.NoError(t, (&lvmClusterValidator{}).verifyAbsolutePath(cluster))
	}
}

func zoneSelector(zone string) *corev1.NodeSelector {
	return &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
		MatchExpressions: []corev1.NodeSelectorRequirement{{
			Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{zone},
		}},
	}}}
}
