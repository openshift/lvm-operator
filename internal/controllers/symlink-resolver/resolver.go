package symlinkResolver

import (
	"path/filepath"
	"sync"
)

type Resolver struct {
	resolveFn ResolveFn
	cache     sync.Map
}

type ResolveFn func(string) (string, error)

var defaultResolverFn = filepath.EvalSymlinks

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
