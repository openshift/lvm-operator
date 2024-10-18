package locking

import (
	"context"
	"fmt"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const VolumeGroupLockName = "shared-volumegroup-lock"

type VolumeGroupLock interface {
	manager.Runnable
	IsLeader() bool
	Check(req *http.Request) error
}

type noneVolumeGroupLock struct{}

func (n *noneVolumeGroupLock) Check(_ *http.Request) error {
	return nil
}

func (n *noneVolumeGroupLock) Start(_ context.Context) error { return nil }
func (n *noneVolumeGroupLock) IsLeader() bool                { return true }

var _ VolumeGroupLock = &noneVolumeGroupLock{}

func NewNoneVolumeGroupLock() VolumeGroupLock {
	return &noneVolumeGroupLock{}
}

type volumegroupLock struct {
	leaderElector   *leaderelection.LeaderElector
	node, namespace string
	healthCheck     healthz.Checker
}

var _ manager.Runnable = &volumegroupLock{}

func NewVolumeGroupLock(ctx context.Context, mgr manager.Manager, node, namespace string) (VolumeGroupLock, error) {
	logger := log.FromContext(ctx)

	leClient, err := kubernetes.NewForConfigAndClient(mgr.GetConfig(), mgr.GetHTTPClient())
	if err != nil {
		return nil, fmt.Errorf("unable to create client for leader election: %w", err)
	}

	wd := leaderelection.NewLeaderHealthzAdaptor(180 * time.Second)

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
				logger.Info("Stopped leading shared volume group creation")
			},
			OnNewLeader: func(identity string) {
				logger.Info("New leader elected for shared volume group creation", "identity", identity)
			},
		},
		WatchDog:        wd,
		ReleaseOnCancel: true,
		Name:            VolumeGroupLockName,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create leader elector: %w", err)
	}

	return &volumegroupLock{
		leaderElector: le,
		healthCheck:   wd.Check,
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

func (s *volumegroupLock) Check(req *http.Request) error {
	if s.healthCheck == nil {
		return nil
	}
	return s.healthCheck(req)
}
