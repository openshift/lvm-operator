package cluster

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/leaderelection"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	snoCheck SNOCheck,
	enableLeaderElection bool,
	operatorNamespace string,
) (LeaderElectionResolver, error) {
	defaultElectionConfig := leaderelection.LeaderElectionDefaulting(configv1.LeaderElection{
		Disable: !enableLeaderElection,
	}, operatorNamespace, "1136b8a6.topolvm.io")

	return &nodeLookupSNOLeaderElection{
		snoCheck:              snoCheck,
		defaultElectionConfig: defaultElectionConfig,
	}, nil
}

type nodeLookupSNOLeaderElection struct {
	snoCheck              SNOCheck
	defaultElectionConfig configv1.LeaderElection
}

func (le *nodeLookupSNOLeaderElection) Resolve(ctx context.Context) (configv1.LeaderElection, error) {
	logger := log.FromContext(ctx)
	if isSNO, err := le.snoCheck.IsSNO(ctx); err != nil {
		return configv1.LeaderElection{}, err
	} else if !isSNO {
		logger.Info("Using default Multi-Node leader election settings optimized for high-availability")
		return le.defaultElectionConfig, nil
	}
	logger.Info("Overwriting defaults with SNO leader election config as only a single node was discovered")
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
