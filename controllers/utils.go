/*
Copyright 2021.

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

package controllers

import (
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// extractNodeSelectorAndTolerations combines and extracts scheduling parameters from the multiple deviceClass entries in an lvmCluster
func extractNodeSelectorAndTolerations(lvmCluster lvmv1alpha1.LVMCluster) (*corev1.NodeSelector, []corev1.Toleration) {
	var nodeSelector *corev1.NodeSelector
	var tolerations []corev1.Toleration
	terms := make([]corev1.NodeSelectorTerm, 0)
	matchAllNodes := false
	for _, deviceClass := range lvmCluster.Spec.DeviceClasses {
		tolerations = append(tolerations, deviceClass.Tolerations...)
		if deviceClass.NodeSelector != nil {
			terms = append(terms, deviceClass.NodeSelector.NodeSelectorTerms...)
		} else {
			matchAllNodes = true
		}
	}
	// populate a nodeSelector unless one or more of the deviceClasses match all nodes with a nil nodeSelector
	if !matchAllNodes {
		nodeSelector = &corev1.NodeSelector{NodeSelectorTerms: terms}
	}
	return nodeSelector, tolerations
}
