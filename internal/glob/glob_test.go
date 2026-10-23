/*
Copyright © 2026 Red Hat, Inc.

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

package glob

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntersects(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"/dev/sda", "/dev/sda", true},
		{"/dev/sda", "/dev/sdb", false},
		{"/dev/sd*", "/dev/sdb", true},
		{"/dev/sd*", "/dev/nvme*", false},
		// Neither pattern matches the other's text, but both match /dev/sda1.
		{"/dev/sd*", "/dev/*1", true},
		{"/dev/sd*", "/dev/sdb*", true},
		{"/dev/sd?", "/dev/sd[b-d]", true},
		{"/dev/sd?", "/dev/sd??", false},
		{"/dev/sd[a-c]", "/dev/sd[c-e]", true},
		{"/dev/sd[a-c]", "/dev/sd[d-f]", false},
		// A star has to bridge more than one character here.
		{"/dev/nvme*n1", "/dev/nvme12*", true},
		{"/dev/nvme*n1", "/dev/nvme2*", true},
		{"/dev/[^a]*", "/dev/a*", false},
		{"/dev/[^a]*", "/dev/b*", true},
		// Only "d" through "w" match both, and neither pattern spells any of them out.
		{"[a-z]", "[^a-cx-z]", true},
		{"/dev/sd[a-z]", "/dev/sd[^a-cx-z]", true},
		// An escape covers the whole rune behind it, not just its first byte.
		{`/dev/sd\é`, `/dev/sd\é`, true},
		{`/dev/sd\é`, "/dev/sd?", true},
		{`/dev/sd\é`, "/dev/sda", false},
		// A star never crosses a separator.
		{"/dev/*", "/dev/disk/by-id/*", false},
		{"/dev/*", "/dev/sda", true},
		{"", "*", true},
		{"", "a", false},
	} {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			got, err := Intersects(tt.a, tt.b)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			// Intersection is symmetric.
			got, err = Intersects(tt.b, tt.a)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIntersectsRejectsMalformedPattern(t *testing.T) {
	// The last three tokenize cleanly; only filepath.Match knows they are malformed.
	for _, pattern := range []string{"/dev/[", "/dev/[a", `/dev/\`, "/dev/[]]", "/dev/[^]", "/dev/sd[a-]"} {
		_, err := Intersects(pattern, "/dev/sda")
		require.ErrorIs(t, err, filepath.ErrBadPattern, pattern)
	}
}

// Cross-check against the real matcher: whenever a concrete device name matches both
// patterns, Intersects must say so. A false negative here is the dangerous direction,
// because it would let two device classes silently claim the same disk.
func TestIntersectsAgreesWithFilepathMatch(t *testing.T) {
	patterns := []string{
		"/dev/sda", "/dev/sd*", "/dev/sd?", "/dev/sd[a-c]", "/dev/sd[c-e]", "/dev/sd*1",
		"/dev/*1", "/dev/*", "/dev/nvme*", "/dev/nvme*n1", "/dev/nvme[0-2]n1", "/dev/nvme2*",
		"/dev/dm-*", "/dev/[^s]*", "/dev/loop?", "/dev/disk/by-id/*",
	}
	names := []string{
		"/dev/sda", "/dev/sdb", "/dev/sdc", "/dev/sdd", "/dev/sde", "/dev/sda1", "/dev/sdb2",
		"/dev/nvme0n1", "/dev/nvme2n1", "/dev/nvme12n1", "/dev/nvme2n1p1", "/dev/dm-0",
		"/dev/loop1", "/dev/loop12", "/dev/disk/by-id/wwn-0x1", "/dev/mapper/crypt",
	}
	for _, a := range patterns {
		for _, b := range patterns {
			var witness string
			for _, name := range names {
				matchedA, err := filepath.Match(a, name)
				require.NoError(t, err)
				matchedB, err := filepath.Match(b, name)
				require.NoError(t, err)
				if matchedA && matchedB {
					witness = name
					break
				}
			}
			if witness == "" {
				continue
			}
			got, err := Intersects(a, b)
			require.NoError(t, err)
			assert.True(t, got, "%q and %q both match %q but were reported disjoint", a, b, witness)
		}
	}
}
