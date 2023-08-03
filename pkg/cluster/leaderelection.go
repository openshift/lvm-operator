package cluster

import (
	"context"
	"fmt"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/leaderelection"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	log "sigs.k8s.io/controller-runtime/pkg/log"
)

// ControlPlaneIDLabel identifies a control plane node by the given label,
// see https://kubernetes.io/docs/reference/labels-annotations-taints/#node-role-kubernetes-io-control-plane
const ControlPlaneIDLabel = "node-role.kubernetes.io/control-plane"

type LeaderElectionResolver interface {
	Resolve(ctx context.Context) (configv1.LeaderElection, error)
}

// NewLeaderElectionResolver returns the correct LeaderElectionResolver Settings for Multi- or SNO-Clusters based
// on the amount of master nodes discovered in the cluster. If there is exactly one control-plane/master node,
// the returned LeaderElectionResolver settings are optimized for SNO deployments.
func NewLeaderElectionResolver(
	config *rest.Config,
	scheme *runtime.Scheme,
	enableLeaderElection bool,
	operatorNamespace string,
) (LeaderElectionResolver, error) {
	leaderElectionClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("cannot create leader election client: %w", err)
	}

	defaultElectionConfig := leaderelection.LeaderElectionDefaulting(configv1.LeaderElection{
		Disable: !enableLeaderElection,
	}, operatorNamespace, "1136b8a6.topolvm.io")

	return &nodeLookupSNOLeaderElection{
		clnt:                  leaderElectionClient,
		defaultElectionConfig: defaultElectionConfig,
	}, nil
}

type nodeLookupSNOLeaderElection struct {
	clnt                  client.Client
	defaultElectionConfig configv1.LeaderElection
}

func (le *nodeLookupSNOLeaderElection) Resolve(ctx context.Context) (configv1.LeaderElection, error) {
	logger := log.FromContext(ctx)
	nodes := &corev1.NodeList{}
	if err := le.clnt.List(context.Background(), nodes, client.MatchingLabels{
		ControlPlaneIDLabel: "",
	}); err != nil {
		logger.Error(err, "unable to retrieve nodes for SNO check with lease configuration")
		os.Exit(1)
	}
	if len(nodes.Items) != 1 {
		return le.defaultElectionConfig, nil
	}
	logger.Info("Overwriting defaults with SNO leader election config as only a single node was discovered",
		"node", nodes.Items[0].GetName())
	config := leaderelection.LeaderElectionSNOConfig(le.defaultElectionConfig)
	logger.Info("leader election config setup succeeded",
		"retry-period", config.RetryPeriod,
		"lease-duration", config.LeaseDuration,
		"renew-deadline", config.RenewDeadline,
		"election-namespace", config.Namespace,
		"election-name", config.Name,
		"disable", config.Disable,
	)
	return config, nil
}
