package selector

import (
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// ExtractNodeSelectorAndTolerations combines and extracts scheduling parameters from the multiple deviceClass entries in an lvmCluster
func ExtractNodeSelectorAndTolerations(lvmCluster *lvmv1alpha1.LVMCluster) (*corev1.NodeSelector, []corev1.Toleration) {
	var nodeSelector *corev1.NodeSelector
	var terms []corev1.NodeSelectorTerm

	for _, deviceClass := range lvmCluster.Spec.Storage.DeviceClasses {
		// if at least one deviceClass has no nodeselector, we must assume that all nodes should be
		// considered for at least one deviceClass and thus we should not add any nodeSelector to the DS
		if deviceClass.NodeSelector == nil {
			return nil, lvmCluster.Spec.Tolerations
		}
		terms = append(terms, deviceClass.NodeSelector.NodeSelectorTerms...)
	}

	if len(terms) > 0 {
		nodeSelector = &corev1.NodeSelector{NodeSelectorTerms: terms}
	}
	return nodeSelector, lvmCluster.Spec.Tolerations
}
