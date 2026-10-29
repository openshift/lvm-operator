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

package nodeselector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func in(key string, values ...string) corev1.NodeSelectorRequirement {
	return corev1.NodeSelectorRequirement{Key: key, Operator: corev1.NodeSelectorOpIn, Values: values}
}

func selector(terms ...corev1.NodeSelectorTerm) *corev1.NodeSelector {
	return &corev1.NodeSelector{NodeSelectorTerms: terms}
}

func expressions(reqs ...corev1.NodeSelectorRequirement) corev1.NodeSelectorTerm {
	return corev1.NodeSelectorTerm{MatchExpressions: reqs}
}

func TestCanIntersect(t *testing.T) {
	for _, tt := range []struct {
		name string
		a, b *corev1.NodeSelector
		want bool
	}{
		{"unset matches every node", nil, selector(expressions(in("zone", "z1"))), true},
		{"both unset", nil, nil, true},
		{"same key, disjoint values", selector(expressions(in("zone", "z1"))), selector(expressions(in("zone", "z2"))), false},
		{"same key, shared value", selector(expressions(in("zone", "z1", "z2"))), selector(expressions(in("zone", "z2"))), true},
		{"identical selectors", selector(expressions(in("zone", "z1"))), selector(expressions(in("zone", "z1"))), true},
		// Nothing rules out a node that is both host a and in zone z1.
		{"different keys", selector(expressions(in("host", "a"))), selector(expressions(in("zone", "z1"))), true},
		// One compatible pair among ORed terms is enough.
		{
			"one of several terms is compatible",
			selector(expressions(in("zone", "z1")), expressions(in("zone", "z2"))),
			selector(expressions(in("zone", "z2"))),
			true,
		},
		{
			"every term pair is disjoint",
			selector(expressions(in("zone", "z1")), expressions(in("zone", "z2"))),
			selector(expressions(in("zone", "z3"))),
			false,
		},
		// Requirements inside a term are ANDed, so one contradiction is enough.
		{
			"one contradiction within an ANDed term",
			selector(expressions(in("zone", "z1"), in("role", "storage"))),
			selector(expressions(in("zone", "z1"), in("role", "compute"))),
			false,
		},
		{"no terms matches no node", selector(), selector(expressions(in("zone", "z1"))), false},
		{
			"match fields are compared too",
			selector(corev1.NodeSelectorTerm{MatchFields: []corev1.NodeSelectorRequirement{in("metadata.name", "node1")}}),
			selector(corev1.NodeSelectorTerm{MatchFields: []corev1.NodeSelectorRequirement{in("metadata.name", "node2")}}),
			false,
		},
		// Operators other than In are not analysed, so the answer stays conservative.
		{
			"unanalysed operators are treated as compatible",
			selector(expressions(corev1.NodeSelectorRequirement{Key: "zone", Operator: corev1.NodeSelectorOpExists})),
			selector(expressions(corev1.NodeSelectorRequirement{Key: "zone", Operator: corev1.NodeSelectorOpDoesNotExist})),
			true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CanIntersect(tt.a, tt.b))
			assert.Equal(t, tt.want, CanIntersect(tt.b, tt.a), "must be symmetric")
		})
	}
}
