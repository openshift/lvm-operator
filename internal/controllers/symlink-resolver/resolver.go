package symlinkResolver

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

type Resolver struct {
	resolveFn ResolveFn
	cache     sync.Map
	pathCache sync.Map
}

type resolvedPaths struct {
	paths []string
	err   error
}

type ResolveFn func(string) (string, error)

var defaultResolverFn = filepath.EvalSymlinks

var ErrGlobNoMatches = errors.New("device glob did not match any paths")

func NewWithDefaultResolver() *Resolver {
	return NewWithResolver(defaultResolverFn)
}

func NewWithResolver(resolveFn ResolveFn) *Resolver {
	if resolveFn == nil {
		resolveFn = defaultResolverFn
	}
	return &Resolver{
		resolveFn: resolveFn,
		cache:     sync.Map{},
	}
}

// Fresh returns a resolver with the same resolution function and an empty cache.
func (r *Resolver) Fresh() *Resolver { return NewWithResolver(r.resolveFn) }

func (r *Resolver) Resolve(path string) (string, error) {
	var err error
	val, ok := r.cache.Load(path)
	if !ok {
		val, err = r.resolveFn(path)
		if err != nil {
			return "", err
		}
		r.cache.Store(path, val)
	}
	return val.(string), nil
}

// ResolvePaths expands glob: selectors and resolves their symlinks. Expansion is
// cached for the lifetime of the resolver so every operation in a reconciliation
// uses the same device set. Literal paths retain their existing semantics.
// When some matches cannot be resolved, it returns the resolved matches and an
// error; optional selectors may still use those matches.
func (r *Resolver) ResolvePaths(path string) ([]string, error) {
	pattern, glob := v1alpha1.DevicePath(path).GlobPattern()
	if !glob {
		resolved, err := r.Resolve(path)
		if err != nil {
			return nil, err
		}
		return []string{resolved}, nil
	}
	if cached, ok := r.pathCache.Load(path); ok {
		result := cached.(resolvedPaths)
		return result.paths, result.err
	}

	result := resolvedPaths{}
	matches, err := filepath.Glob(pattern)
	switch {
	case err != nil:
		result.err = fmt.Errorf("invalid device glob %q: %w", path, err)
	case len(matches) == 0:
		result.err = fmt.Errorf("%w: %q", ErrGlobNoMatches, path)
	default:
		for _, match := range matches {
			resolved, err := r.Resolve(match)
			if err != nil {
				result.err = errors.Join(result.err, fmt.Errorf("failed to resolve glob match %q: %w", match, err))
				continue
			}
			if !slices.Contains(result.paths, resolved) {
				result.paths = append(result.paths, resolved)
			}
		}
	}
	r.pathCache.Store(path, result)
	return result.paths, result.err
}
