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
	"regexp"
	"strings"
)

// DevicePathRegexPrefix marks a DevicePath as a regular expression that is matched
// against the kernel device names discovered on a node, instead of a literal path
// that is resolved through symlinks.
const DevicePathRegexPrefix = "regex:"

// IsRegex reports whether the path is a regular expression pattern.
func (d DevicePath) IsRegex() bool {
	return strings.HasPrefix(string(d), DevicePathRegexPrefix)
}

// Pattern returns the anchored regular expression source of a regex path, and an empty
// string for a literal path.
//
// The user pattern is wrapped in a non-capturing group before anchoring so that a
// top-level alternation such as "/dev/sda|/dev/sdb" anchors both branches. Anchoring
// without the group would produce "^/dev/sda|/dev/sdb$", which matches any name merely
// starting with /dev/sda.
func (d DevicePath) Pattern() string {
	if !d.IsRegex() {
		return ""
	}
	return "^(?:" + strings.TrimPrefix(string(d), DevicePathRegexPrefix) + ")$"
}

// Compile compiles the anchored pattern of a regex path. It returns an error for a
// literal path, because callers must branch on IsRegex before compiling.
func (d DevicePath) Compile() (*regexp.Regexp, error) {
	if !d.IsRegex() {
		return nil, fmt.Errorf("device path %q is not a regex path", d)
	}
	return regexp.Compile(d.Pattern())
}

// Matches reports whether a regex path selects the given kernel device name. A literal
// path never matches, because literal paths are compared after symlink resolution.
func (d DevicePath) Matches(kname string) (bool, error) {
	if !d.IsRegex() {
		return false, nil
	}
	re, err := d.Compile()
	if err != nil {
		return false, err
	}
	return re.MatchString(kname), nil
}
