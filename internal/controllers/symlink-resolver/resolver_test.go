package symlinkResolver

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

func TestNewWithDefaultResolver(t *testing.T) {
	resolver := NewWithDefaultResolver()
	assert.Assert(t, resolver != nil)
	assert.Assert(t, resolver.resolveFn != nil)
}

func TestResolvePaths(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	for _, name := range []string{"sda", "sdb", "nvme0n1", "sd*"} {
		assert.NilError(t, os.WriteFile(filepath.Join(dir, name), nil, 0600))
	}
	assert.NilError(t, os.Symlink(filepath.Join(dir, "sda"), filepath.Join(dir, "alias-a")))
	assert.NilError(t, os.Symlink(filepath.Join(dir, "sda"), filepath.Join(dir, "alias-b")))
	assert.NilError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "alias-broken")))

	for _, tc := range []struct {
		name    string
		path    string
		want    []string
		wantErr bool
	}{
		{name: "literal wildcard", path: filepath.Join(dir, "sd*"), want: []string{filepath.Join(dir, "sd*")}},
		{name: "multiple matches", path: "glob:" + filepath.Join(dir, "sd[a-b]"), want: []string{filepath.Join(dir, "sda"), filepath.Join(dir, "sdb")}},
		{name: "no matches", path: "glob:" + filepath.Join(dir, "vd*"), wantErr: true},
		{name: "invalid pattern", path: "glob:" + filepath.Join(dir, "sd["), wantErr: true},
		{name: "symlinks deduplicated", path: "glob:" + filepath.Join(dir, "alias-[ab]"), want: []string{filepath.Join(dir, "sda")}},
		{name: "broken symlink preserves other matches", path: "glob:" + filepath.Join(dir, "alias-*"), want: []string{filepath.Join(dir, "sda")}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewWithDefaultResolver().ResolvePaths(tc.path)
			assert.Equal(t, err != nil, tc.wantErr)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

func TestResolvePathsCache(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	first, second := filepath.Join(dir, "sda"), filepath.Join(dir, "sdb")
	assert.NilError(t, os.WriteFile(first, nil, 0600))
	resolver := NewWithDefaultResolver()
	pattern := "glob:" + filepath.Join(dir, "sd*")
	paths, err := resolver.ResolvePaths(pattern)
	assert.NilError(t, err)
	assert.DeepEqual(t, paths, []string{first})
	assert.NilError(t, os.WriteFile(second, nil, 0600))
	paths, err = resolver.ResolvePaths(pattern)
	assert.NilError(t, err)
	assert.DeepEqual(t, paths, []string{first})
	paths, err = NewWithDefaultResolver().ResolvePaths(pattern)
	assert.NilError(t, err)
	assert.DeepEqual(t, paths, []string{first, second})
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
