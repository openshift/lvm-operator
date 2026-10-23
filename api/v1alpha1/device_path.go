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

package v1alpha1

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/openshift/lvm-operator/v4/internal/glob"
)

const devicePathGlobPrefix = "glob:"

// IsGlob reports whether the path is a shell-style pattern rather than a literal path.
func (d DevicePath) IsGlob() bool {
	return strings.HasPrefix(string(d), devicePathGlobPrefix)
}

// Pattern returns the glob without its prefix.
func (d DevicePath) Pattern() string {
	return strings.TrimPrefix(string(d), devicePathGlobPrefix)
}

// Match reports whether a kernel device name matches the glob. Syntax is filepath.Match,
// the same as a shell: "*" never crosses a "/" and "." is a literal dot. Regular
// expression syntax is deliberately not supported, because "/dev/sd.*" selecting every
// SCSI disk is indistinguishable at a glance from "/dev/sd*" selecting none.
func (d DevicePath) Match(kname string) (bool, error) {
	matched, err := filepath.Match(d.Pattern(), kname)
	if err != nil {
		return false, fmt.Errorf("invalid device path pattern %q: %w", d, err)
	}
	return matched, nil
}

// Validate reports whether a glob is well-formed. Literal paths always validate.
func (d DevicePath) Validate() error {
	if !d.IsGlob() {
		return nil
	}
	_, err := d.Match("/dev/validate")
	return err
}

// Overlaps reports whether two selectors can ever select the same kernel device name.
// Two literal paths overlap only when identical, a pattern and a literal overlap when
// the pattern matches it, and two patterns overlap when the sets of names they describe
// intersect — decidable from the patterns alone, without knowing any node's devices.
func (d DevicePath) Overlaps(other DevicePath) (bool, error) {
	switch {
	case !d.IsGlob() && !other.IsGlob():
		return d == other, nil
	case !d.IsGlob():
		return other.Match(d.Unresolved())
	case !other.IsGlob():
		return d.Match(other.Unresolved())
	}
	overlaps, err := glob.Intersects(d.Pattern(), other.Pattern())
	if err != nil {
		return false, fmt.Errorf("invalid device path pattern %w", err)
	}
	return overlaps, nil
}

// HasGlobPaths reports whether required or optional selectors contain a pattern.
func (s *DeviceSelector) HasGlobPaths() bool {
	if s == nil {
		return false
	}
	for _, paths := range [][]DevicePath{s.Paths, s.OptionalPaths} {
		for _, path := range paths {
			if path.IsGlob() {
				return true
			}
		}
	}
	return false
}
