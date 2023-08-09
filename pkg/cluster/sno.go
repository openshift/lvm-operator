package cluster

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type SNOCheck interface {
	IsSNO(ctx context.Context) bool
}

func NewMasterSNOCheck(clnt client.Client) SNOCheck {
	return &masterSNOCheck{clnt: clnt}
}

type masterSNOCheck struct {
	clnt client.Client
}

func (chk *masterSNOCheck) IsSNO(ctx context.Context) bool {
	logger := log.FromContext(ctx)
	nodes := &corev1.NodeList{}
	if err := chk.clnt.List(context.Background(), nodes, client.MatchingLabels{
		ControlPlaneIDLabel: "",
	}); err != nil {
		logger.Error(err, "unable to retrieve nodes for SNO check with lease configuration")
		os.Exit(1)
	}
	return nodes.Items != nil && len(nodes.Items) == 1
}
