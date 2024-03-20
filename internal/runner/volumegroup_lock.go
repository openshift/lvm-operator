package runner

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const VolumeGroupLockName = "shared-volumegroup-lock"

type VolumeGroupLock interface {
	manager.Runnable
	IsLeader() bool
}

type noneVolumeGroupLock struct{}

func (n *noneVolumeGroupLock) Start(_ context.Context) error { return nil }
func (n *noneVolumeGroupLock) IsLeader() bool                { return true }

var _ VolumeGroupLock = &noneVolumeGroupLock{}

func NewNoneVolumeGroupLock() VolumeGroupLock {
	return &noneVolumeGroupLock{}
}

type volumegroupLock struct {
	leaderElector   *leaderelection.LeaderElector
	node, namespace string
}

var _ manager.Runnable = &sanlockRunner{}

func NewVolumeGroupLock(mgr manager.Manager, node, namespace string) (VolumeGroupLock, error) {
	leClient, err := kubernetes.NewForConfigAndClient(mgr.GetConfig(), mgr.GetHTTPClient())
	if err != nil {
		return nil, fmt.Errorf("unable to create client for leader election: %w", err)
	}

	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      VolumeGroupLockName,
				Namespace: namespace,
			},
			Client: leClient.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity:      node,
				EventRecorder: mgr.GetEventRecorderFor(VolumeGroupLockName),
			},
		},
		LeaseDuration: 180 * time.Second, // aligned to sanlock timeout
		RenewDeadline: 60 * time.Second,
		RetryPeriod:   30 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				log.FromContext(ctx).Info("Started leading shared volume group creation")
			},
			OnStoppedLeading: func() {
				log.Log.Info("Stopped leading shared volume group creation")
			},
			OnNewLeader: func(identity string) {
				log.Log.Info("New leader elected for shared volume group creation", "identity", identity)
			},
		},
		WatchDog:        leaderelection.NewLeaderHealthzAdaptor(10 * time.Second),
		ReleaseOnCancel: true,
		Name:            VolumeGroupLockName,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create leader elector: %w", err)
	}

	return &volumegroupLock{
		leaderElector: le,
	}, nil
}

// Start implements controller-runtime's manager.Runnable.
func (s *volumegroupLock) Start(ctx context.Context) error {
	s.leaderElector.Run(ctx)
	return nil
}

func (s *volumegroupLock) IsLeader() bool {
	return s.leaderElector.IsLeader()
}
