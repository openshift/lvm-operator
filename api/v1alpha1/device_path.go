package v1alpha1

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// GlobPattern returns the filesystem pattern for a glob device selector.
// Paths without the glob: prefix are always treated literally.
func (d DevicePath) GlobPattern() (string, bool) {
	return strings.CutPrefix(d.Unresolved(), "glob:")
}

func (d DevicePath) IsGlob() bool { _, ok := d.GlobPattern(); return ok }

func (d DevicePath) Pattern() string { pattern, _ := d.GlobPattern(); return pattern }

func (d DevicePath) Match(name string) (bool, error) {
	matched, err := filepath.Match(d.Pattern(), name)
	if err != nil {
		return false, fmt.Errorf("invalid device path pattern %q: %w", d, err)
	}
	return matched, nil
}

func (d DevicePath) Validate() error {
	if !d.IsGlob() {
		return nil
	}
	_, err := d.Match("/dev/validate")
	return err
}

// ValidateLocation reports whether the path names a device under /dev. It is the
// same rule the CRD pattern marker enforces, kept in Go as well because the
// traversal check needs filepath.Clean, which an OpenAPI pattern cannot express.
// Clean is applied to literals too, so "/dev/../etc/shadow" is refused and not
// merely pattern-matched.
func (d DevicePath) ValidateLocation() error {
	pattern, glob := d.GlobPattern()
	cleaned := filepath.Clean(pattern)
	if !strings.HasPrefix(pattern, "/dev/") || !strings.HasPrefix(cleaned, "/dev/") {
		return errors.New("must be an absolute path to the device under /dev/")
	}
	// filepath.Match neither collapses separators nor resolves "." and "..", so a
	// non-canonical pattern selects nothing at all. Refusing it reports that to the
	// admin instead of silently matching no devices, and it leaves overlap checking
	// one spelling per device to compare.
	if glob && cleaned != pattern {
		return fmt.Errorf("device path pattern %q would never match any device because it is not canonical; write it as %q", pattern, cleaned)
	}
	return nil
}

// Overlaps compares literals in canonical form, because "/dev/sda" and
// "/dev/disk/../sda" name one disk and must not pass as two. Patterns are already
// canonical by the time admission compares them, see ValidateLocation.
func (d DevicePath) Overlaps(other DevicePath) (bool, error) {
	switch {
	case d == other:
		return true, nil
	case !d.IsGlob() && !other.IsGlob():
		return filepath.Clean(d.Unresolved()) == filepath.Clean(other.Unresolved()), nil
	case !d.IsGlob():
		return other.Match(filepath.Clean(d.Unresolved()))
	case !other.IsGlob():
		return d.Match(filepath.Clean(other.Unresolved()))
	default:
		return false, nil
	}
}

// MayOverlap answers the same question as Overlaps but compares two patterns
// instead of admitting them. Overlaps defers that case to the node, which is safe
// while ownership decides the winner: the loser simply goes without the disk.
// Force wipe runs before any ownership exists, so the first volume group to
// reconcile destroys the disk the other was configured for, unrecoverably. That
// cannot be deferred to the node, so admission has to answer it here.
func (d DevicePath) MayOverlap(other DevicePath) (bool, error) {
	if overlaps, err := d.Overlaps(other); overlaps || err != nil {
		return overlaps, err
	}
	if !d.IsGlob() || !other.IsGlob() {
		return false, nil
	}
	return patternsMayOverlap(d.Pattern(), other.Pattern()), nil
}

// patternsMayOverlap reports false only when two patterns provably cannot match
// the same path. filepath.Match never lets a wildcard cross a separator, so
// patterns of differing segment counts cannot collide, and neither can two whose
// segments diverge. Anything it cannot rule out is reported as overlapping.
func patternsMayOverlap(a, b string) bool {
	// Admission refuses non-canonical patterns before reaching here, but segment
	// counts decide the answer, so normalise anyway rather than let a stray
	// separator report two colliding patterns as disjoint.
	aSegments, bSegments := strings.Split(filepath.Clean(a), "/"), strings.Split(filepath.Clean(b), "/")
	if len(aSegments) != len(bSegments) {
		return false
	}
	for i := range aSegments {
		if !segmentsMayOverlap(aSegments[i], bSegments[i]) {
			return false
		}
	}
	return true
}

// segmentsMayOverlap compares the literal text each segment must begin with. Two
// segments whose fixed prefixes diverge cannot match one name whatever their
// wildcards expand to, so "sd*" and "nvme*" are provably disjoint while "nvme*"
// and "nvme2*" are not. Wildcards beyond the prefix are not analysed, so disjoint
// character classes are reported as overlapping rather than picked apart.
func segmentsMayOverlap(a, b string) bool {
	aPrefix, aWild := literalPrefix(a)
	bPrefix, bWild := literalPrefix(b)
	switch {
	case !aWild && !bWild:
		return a == b
	case !aWild:
		return strings.HasPrefix(aPrefix, bPrefix)
	case !bWild:
		return strings.HasPrefix(bPrefix, aPrefix)
	default:
		return strings.HasPrefix(aPrefix, bPrefix) || strings.HasPrefix(bPrefix, aPrefix)
	}
}

// literalPrefix returns the text before the first wildcard, and whether one exists.
func literalPrefix(segment string) (string, bool) {
	if i := strings.IndexAny(segment, `*?[\`); i >= 0 {
		return segment[:i], true
	}
	return segment, false
}

// HasGlobPaths reports whether the selector includes any glob patterns.
func (d *DeviceSelector) HasGlobPaths() bool {
	if d == nil {
		return false
	}
	for _, paths := range [][]DevicePath{d.Paths, d.OptionalPaths} {
		for _, path := range paths {
			if _, glob := path.GlobPattern(); glob {
				return true
			}
		}
	}
	return false
}
