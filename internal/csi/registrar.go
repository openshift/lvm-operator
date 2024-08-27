package csi

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"sync"
	"time"

	"k8s.io/klog/v2"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// registrationServer is a sample plugin to work with plugin watcher
type registrationServer struct {
	driverName string
	endpoint   string
	version    []string

	watchDogTimeout time.Duration

	registered    bool
	registeredMut sync.RWMutex

	onFail context.CancelCauseFunc
}

var _ registerapi.RegistrationServer = &registrationServer{}

var ErrPluginRegistrationFailed = fmt.Errorf("plugin registration failed")

type CheckableRegistrationServer interface {
	registerapi.RegistrationServer
	Registered() bool
	StartRegistrationWatchdog(ctx context.Context)
}

// NewRegistrationServer returns an initialized registrationServer instance
func NewRegistrationServer(
	onFail context.CancelCauseFunc,
	driverName string,
	endpoint string,
	versions []string,
	watchDogTimeout time.Duration,
) CheckableRegistrationServer {
	return &registrationServer{
		watchDogTimeout: watchDogTimeout,
		onFail:          onFail,
		driverName:      driverName,
		endpoint:        endpoint,
		version:         versions,
	}
}

// StartRegistrationWatchdog starts a watchdog to check if the plugin is registered after e.watchDogTimeout
// If the plugin is not registered, it will call e.onFail
// If the plugin is registered, it will return on the next opportunity
func (e *registrationServer) StartRegistrationWatchdog(ctx context.Context) {
	go func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, e.watchDogTimeout)
		defer cancel()
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			if e.Registered() {
				// registered, happy path
				return
			}
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				e.onFail(fmt.Errorf("%w: %s", ErrPluginRegistrationFailed, err))
			}
		}
	}(ctx)
}

// GetInfo is the RPC invoked by plugin watcher
func (e *registrationServer) GetInfo(ctx context.Context, req *registerapi.InfoRequest) (*registerapi.PluginInfo, error) {
	klog.V(2).Infof("Received GetInfo call (signalling the beginning of registration): %+v", req)

	return &registerapi.PluginInfo{
		Type:              registerapi.CSIPlugin,
		Name:              e.driverName,
		Endpoint:          e.endpoint,
		SupportedVersions: e.version,
	}, nil
}

func (e *registrationServer) NotifyRegistrationStatus(ctx context.Context, status *registerapi.RegistrationStatus) (*registerapi.RegistrationStatusResponse, error) {
	log.FromContext(ctx).Info("CSI Plugin got a registration update", "status", status)
	if !status.PluginRegistered {
		err := fmt.Errorf("%w: %s", ErrPluginRegistrationFailed, status.Error)
		log.FromContext(ctx).Error(err, "Registration process failed, restarting registration container.")
		defer e.onFail(err)
		return &registerapi.RegistrationStatusResponse{}, nil
	}

	e.registeredMut.Lock()
	e.registered = status.PluginRegistered
	e.registeredMut.Unlock()

	return &registerapi.RegistrationStatusResponse{}, nil
}

func (e *registrationServer) Registered() bool {
	e.registeredMut.RLock()
	defer e.registeredMut.RUnlock()
	return e.registered
}
