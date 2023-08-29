package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	configv1 "github.com/openshift/api/config/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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
func (r DefaultTypeResolver) GetType(ctx context.Context) (Type, error) {
	logger := log.FromContext(ctx)

	err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, &configv1.Infrastructure{})

	if err == nil {
		logger.Info("Openshift Infrastructure found, setting cluster type to openshift")
		return TypeOCP, nil
	}

	if k8serrors.IsNotFound(err) {
		logger.Info("Openshift Infrastructure not found, setting cluster type to other")
		return TypeOther, nil
	}

	// Introduced in controller-runtime v0.15.0, which makes a simple
	// `k8serrors.IsNotFound(err)` not work any more
	// see https://github.com/kubernetes-sigs/controller-runtime/issues/2354
	groupErr := &discovery.ErrGroupDiscoveryFailed{}
	if errors.As(err, &groupErr) {
		for _, err := range groupErr.Groups {
			if k8serrors.IsNotFound(err) {
				logger.Info("Openshift Infrastructure not available in the cluster, setting cluster type to other")
				return TypeOther, nil
			}
		}
	}

	return "", fmt.Errorf("failed to get Openshift Infrastructure 'cluster': %w", err)
}
