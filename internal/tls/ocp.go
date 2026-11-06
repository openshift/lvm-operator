/*
Copyright © 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tls

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/openshift/api/config/v1"
	ctrlRuntimeCommon "github.com/openshift/controller-runtime-common/pkg/tls"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Setup encapsulates TLS configuration for OCP and non-OCP clusters.
type Setup struct {
	clusterType cluster.Type
	tlsProfile  v1.TLSProfileSpec
	tlsOpts     []func(*tls.Config)
}

// Resolve determines and caches TLS configuration based on cluster type.
// For OCP clusters, it fetches the APIServer TLS profile.
// For non-OCP clusters, it uses Go's default TLS configuration.
func Resolve(ctx context.Context, clusterType cluster.Type, setupClient client.Client, setupLog logr.Logger) (*Setup, error) {
	setup := &Setup{
		clusterType: clusterType,
		tlsOpts: []func(*tls.Config){
			func(c *tls.Config) { c.NextProtos = []string{"http/1.1"} },
		},
	}

	if clusterType == cluster.TypeOCP {
		profile, err := ctrlRuntimeCommon.FetchAPIServerTLSProfile(ctx, setupClient)
		if err != nil {
			return nil, fmt.Errorf("failed to get tls profile: %w", err)
		}

		tlsConfig, unsupportedCiphers := ctrlRuntimeCommon.NewTLSConfigFromProfile(profile)
		if len(unsupportedCiphers) > 0 {
			setupLog.Info("some ciphers from TLS profile are not supported", "unsupportedCiphers", unsupportedCiphers)
		}

		setup.tlsProfile = profile
		setup.tlsOpts = append(setup.tlsOpts, tlsConfig)
	}

	return setup, nil
}

// Options returns the TLS configuration options for use in controller-runtime manager.
func (s *Setup) Options() []func(*tls.Config) {
	return s.tlsOpts
}

// SetupWatcher registers the TLS SecurityProfileWatcher on OCP clusters.
// onProfileChange is called when the TLS profile changes and should trigger a shutdown.
func (s *Setup) SetupWatcher(mgr manager.Manager, onProfileChange func(context.Context, v1.TLSProfileSpec, v1.TLSProfileSpec)) error {
	if s.clusterType != cluster.TypeOCP {
		return nil
	}

	tlsWatcherController := &ctrlRuntimeCommon.SecurityProfileWatcher{
		Client:                mgr.GetClient(),
		InitialTLSProfileSpec: s.tlsProfile,
		OnProfileChange:       onProfileChange,
	}

	return tlsWatcherController.SetupWithManager(mgr)
}
