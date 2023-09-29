package selector

import (
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// ExtractNodeSelectorAndTolerations combines and extracts scheduling parameters from the multiple deviceClass entries in an lvmCluster
func ExtractNodeSelectorAndTolerations(lvmCluster *lvmv1alpha1.LVMCluster) (*corev1.NodeSelector, []corev1.Toleration) {
	var nodeSelector *corev1.NodeSelector

	tolerations := lvmCluster.Spec.Tolerations

	terms := make([]corev1.NodeSelectorTerm, 0)
	matchAllNodes := false
	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {

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
