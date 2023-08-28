package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	v1 "github.com/openshift/api/security/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const openshiftSCCPrivilegedName = "privileged"

type Type string

const (
	TypeOCP   Type = "openshift"
	TypeOther Type = "other"
)

type TypeResolver interface {
	GetType(ctx context.Context) (Type, error)
}

func NewTypeResolver(clnt client.Client) TypeResolver {
	return &CachedTypeResolver{
		TypeResolver: DefaultTypeResolver{clnt},
		cached:       atomic.Pointer[Type]{},
	}
}

type CachedTypeResolver struct {
	TypeResolver
	cached atomic.Pointer[Type]
}

func (r *CachedTypeResolver) GetType(ctx context.Context) (Type, error) {
	if r.cached.Load() == nil {
		t, err := r.TypeResolver.GetType(ctx)
		if err != nil {
			return "", err
		}
		r.cached.Store(&t)
	}
	return *r.cached.Load(), nil
}

type DefaultTypeResolver struct{ client.Client }

// GetType checks to see if the operator is running on an OCP cluster.
// It does this by querying for the "privileged" SCC which exists on all OCP clusters.
func (r DefaultTypeResolver) GetType(ctx context.Context) (Type, error) {
	logger := log.FromContext(ctx)
	// cluster type has not been determined yet
	// Check if the privileged SCC exists on the cluster (this is one of the default SCCs)
	err := r.Get(ctx, types.NamespacedName{Name: openshiftSCCPrivilegedName}, &v1.SecurityContextConstraints{})

	if err == nil {
		logger.Info("openshiftSCC found, setting cluster type to openshift")
		return TypeOCP, nil
	}

	if k8serrors.IsNotFound(err) {
		// Not an Openshift cluster
		logger.Info("openshiftSCC not found, setting cluster type to other")
		return TypeOther, nil
	}

	// Introduced in controller-runtime v0.15.0, which makes a simple
	// `k8serrors.IsNotFound(err)` not work any more
	// see https://github.com/kubernetes-sigs/controller-runtime/issues/2354
	groupErr := &discovery.ErrGroupDiscoveryFailed{}
	if errors.As(err, &groupErr) {
		for _, err := range groupErr.Groups {
			if k8serrors.IsNotFound(err) {
				logger.Info("SCCs are not available in the cluster, setting cluster type to other")
				return TypeOther, nil
			}
		}
	}

	return "", fmt.Errorf("failed to get SCC %s: %w", openshiftSCCPrivilegedName, err)
}
