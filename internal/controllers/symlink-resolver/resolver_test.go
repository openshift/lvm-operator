package symlinkResolver

import (
	"fmt"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

func TestNewWithDefaultResolver(t *testing.T) {
	resolver := NewWithDefaultResolver()
	assert.Assert(t, resolver != nil)
	assert.Assert(t, resolver.resolveFn != nil)
}

func TestNewWithResolver(t *testing.T) {
	resolver := NewWithResolver(func(s string) (string, error) {
		return "super-test", nil
	})
	assert.Assert(t, resolver != nil)

	path, err := resolver.Resolve("test")
	assert.NilError(t, err)
	assert.Equal(t, "super-test", path)
}

func TestResolver_Resolve(t *testing.T) {

	tests := []struct {
		name      string
		resolveFn ResolveFn
		args      string
		want      string
		wantErr   bool
	}{
		{
			name:      "successful resolution",
			resolveFn: func(s string) (string, error) { return s, nil },
			args:      "1",
			want:      "1",
			wantErr:   false,
		},
		{
			name:      "resolve error",
			resolveFn: func(s string) (string, error) { return "", fmt.Errorf("test error") },
			args:      "1",
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resolver{
				resolveFn: tt.resolveFn,
				cache:     sync.Map{},
			}
			got, err := r.Resolve(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Resolve() got = %v, want %v", got, tt.want)
			}

			if !tt.wantErr {
				val, ok := r.cache.Load("1")
				assert.Assert(t, ok)
				assert.Equal(t, val, tt.want)
			}
		})
	}
}
