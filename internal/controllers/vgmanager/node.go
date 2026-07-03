package vgmanager

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func matchesNode(ctx context.Context, c client.Reader, nodeName string, selector *corev1.NodeSelector) (bool, error) {
	node := &corev1.Node{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}), node); err != nil {
		return false, err
	}
	if selector == nil {
		return true, nil
	}
	return corev1helper.MatchNodeSelectorTerms(node, selector)
}
