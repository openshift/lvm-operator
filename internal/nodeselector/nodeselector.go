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

// Package nodeselector decides whether two node selectors can both match one node.
//
// It answers conservatively: true unless the two are provably disjoint. Admission uses
// this to decide whether two device classes need their device selectors compared for
// overlap. A wrong "false" lets two volume groups claim the same disk; a wrong "true"
// only asks someone to resolve a device selector conflict. Node labels are mutable in
// any case, so selectors that miss each other today can meet tomorrow.
package nodeselector

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
)

// CanIntersect reports whether some node could satisfy both selectors.
func CanIntersect(a, b *corev1.NodeSelector) bool {
	// A nil selector is not set, so the device class applies to every node. A selector
	// with no terms matches no node, which the loops below report as disjoint.
	if a == nil || b == nil {
		return true
	}
	// Terms are ORed, so one compatible pair is enough.
	for _, termA := range a.NodeSelectorTerms {
		for _, termB := range b.NodeSelectorTerms {
			if !splitByValue(termA.MatchExpressions, termB.MatchExpressions) &&
				!splitByValue(termA.MatchFields, termB.MatchFields) {
				return true
			}
		}
	}
	return false
}

// splitByValue recognises the one contradiction real configurations rely on: two
// requirements on the same key that admit no common value, which is how nodes get split
// by zone, rack, role or name. Anything subtler is reported as compatible.
func splitByValue(a, b []corev1.NodeSelectorRequirement) bool {
	for _, first := range a {
		for _, second := range b {
			if first.Key != second.Key ||
				first.Operator != corev1.NodeSelectorOpIn || second.Operator != corev1.NodeSelectorOpIn {
				continue
			}
			if !slices.ContainsFunc(first.Values, func(value string) bool {
				return slices.Contains(second.Values, value)
			}) {
				return true
			}
		}
	}
	return false
}
