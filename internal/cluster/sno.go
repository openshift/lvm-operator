package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SNOCheck interface {
	IsSNO(ctx context.Context) (bool, error)
}

func NewMasterSNOCheck(clnt client.Client) SNOCheck {
	return &masterSNOCheck{clnt: clnt}
}

type masterSNOCheck struct {
	clnt client.Client
}

func (chk *masterSNOCheck) IsSNO(ctx context.Context) (bool, error) {
	nodes := &corev1.NodeList{}
	if err := chk.clnt.List(ctx, nodes, client.MatchingLabels{
		ControlPlaneIDLabel: "",
	}); err != nil {
		return false, fmt.Errorf("unable to retrieve nodes for SNO check with lease configuration: %w", err)

	}
	return len(nodes.Items) == 1, nil
}
