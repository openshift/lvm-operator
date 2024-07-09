package resource

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/logpassthrough"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Reconciler interface {
	client.Client

	GetNamespace() string
	GetImageName() string
	SnapshotsEnabled() bool
	GetVGManagerCommand() []string

	// GetTopoLVMLeaderElectionPassthrough uses the given leaderElection when initializing TopoLVM to synchronize
	// leader election configuration
	GetTopoLVMLeaderElectionPassthrough() configv1.LeaderElection

	// GetLogPassthroughOptions passes log information for resource managers to consume
	GetLogPassthroughOptions() *logpassthrough.Options
}

// Manager NOTE: when updating this, please also update docs/design/lvm-operator-manager.md
type Manager interface {

	// GetName should return a camelCase name of this unit of reconciliation
	GetName() string

	// EnsureCreated should check the resources managed by this unit
	EnsureCreated(Reconciler, context.Context, *lvmv1alpha1.LVMCluster) error

	// EnsureDeleted should wait for the resources to be cleaned up
	EnsureDeleted(Reconciler, context.Context, *lvmv1alpha1.LVMCluster) error
}
