package selector

import (
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
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

func ValidNodes(lvmCluster *lvmv1alpha1.LVMCluster, nodes *corev1.NodeList) ([]corev1.Node, error) {
	var validNodes []corev1.Node
	nodeSelector, tolerations := ExtractNodeSelectorAndTolerations(lvmCluster)

	for _, node := range nodes.Items {
		// Check if node tolerates all taints
		if !toleratesAllTaints(node.Spec.Taints, tolerations) {
			continue
		}

		// If no node selector is specified, the node is valid
		if nodeSelector == nil {
			validNodes = append(validNodes, node)
			continue
		}

		// Check if the node matches the node selector terms
		if matches, err := corev1helper.MatchNodeSelectorTerms(&node, nodeSelector); err != nil {
			return nil, err
		} else if matches {
			validNodes = append(validNodes, node)
		}
	}

	return validNodes, nil
}

// tolerateAllTaints returns true if all taints are tolerated by the provided tolerations
func toleratesAllTaints(taints []corev1.Taint, tolerations []corev1.Toleration) bool {
	for _, taint := range taints {
		if !corev1helper.TolerationsTolerateTaint(tolerations, &taint) {
			return false
		}
	}
	return true
}
