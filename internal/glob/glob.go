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

// Package glob decides whether two shell-style patterns can select the same name.
//
// filepath.Match answers "does this pattern match this name". There is no standard
// answer for "can these two patterns match a common name", which is what admission
// needs in order to reject overlapping device selectors before any node sees them.
//
// Testing each pattern against the other's text is not an answer: "/dev/sd*" and
// "/dev/*1" both match "/dev/sda1" while neither matches the other literally. This
// package uses the usual algorithm for the problem instead — split both patterns into
// units and walk the two sequences as a product automaton, in O(len(a)*len(b)).
package glob

import (
	"fmt"
	"path/filepath"
	"unicode/utf8"
)

// Intersects reports whether any single name matches both patterns. Pattern syntax is
// filepath.Match's, so "*" and "?" never cross a Separator.
func Intersects(a, b string) (bool, error) {
	left, err := units(a)
	if err != nil {
		return false, err
	}
	right, err := units(b)
	if err != nil {
		return false, err
	}
	return intersects(left, right, alphabet(a, b)), nil
}

// units splits a pattern into units, each either "*" or something consuming exactly one
// character: a literal, "?", an escape, or a "[...]" class. What a unit accepts is left
// to filepath.Match rather than reimplemented here.
func units(pattern string) ([]string, error) {
	var split []string
	for i := 0; i < len(pattern); {
		start := i
		switch pattern[i] {
		case '*':
			for i < len(pattern) && pattern[i] == '*' {
				i++ // a run of stars is equivalent to one
			}
			split = append(split, "*")
			continue
		case '[':
			for i++; i < len(pattern) && pattern[i] != ']'; i++ {
				if pattern[i] == '\\' {
					i++
				}
			}
			i++ // closing ']'
		case '\\':
			i += 2
		default:
			_, size := utf8.DecodeRuneInString(pattern[i:])
			i += size
		}
		if i > len(pattern) {
			return nil, fmt.Errorf("%q: %w", pattern, filepath.ErrBadPattern)
		}
		split = append(split, pattern[start:i])
	}
	return split, nil
}

// alphabet returns enough characters to decide any "can these two units accept the same
// character" question: every character either pattern mentions, plus one stand-in for
// everything neither mentions. Two overlapping ranges always share an endpoint of one of
// them, so range interiors need no representative of their own.
func alphabet(patterns ...string) []rune {
	seen := map[rune]bool{filepath.Separator: true}
	for _, pattern := range patterns {
		for _, r := range pattern {
			seen[r] = true
		}
	}
	runes := make([]rune, 0, len(seen)+1)
	for r := range seen {
		runes = append(runes, r)
	}
	for r := 'a'; ; r++ {
		if !seen[r] {
			return append(runes, r)
		}
	}
}

// intersects walks the two unit sequences as a product automaton. Every branch advances
// at least one index, so the recursion terminates.
func intersects(a, b []string, runes []rune) bool {
	accepts := func(unit string, r rune) bool {
		matched, err := filepath.Match(unit, string(r))
		return err == nil && matched
	}
	// Can this unit produce a character that a star is able to absorb?
	absorbable := func(unit string) bool {
		for _, r := range runes {
			if r != filepath.Separator && accepts(unit, r) {
				return true
			}
		}
		return false
	}
	shareCharacter := func(x, y string) bool {
		for _, r := range runes {
			if accepts(x, r) && accepts(y, r) {
				return true
			}
		}
		return false
	}
	allStars := func(units []string) bool {
		for _, unit := range units {
			if unit != "*" {
				return false
			}
		}
		return true
	}

	memo := make(map[[2]int]bool, len(a)*len(b))
	var match func(i, j int) bool
	match = func(i, j int) bool {
		switch {
		case i == len(a):
			return allStars(b[j:])
		case j == len(b):
			return allStars(a[i:])
		}
		key := [2]int{i, j}
		if cached, ok := memo[key]; ok {
			return cached
		}
		var result bool
		switch {
		case a[i] == "*" && b[j] == "*":
			result = match(i+1, j) || match(i, j+1)
		case a[i] == "*":
			// The star matches nothing, or it absorbs the character b produces here.
			result = match(i+1, j) || (absorbable(b[j]) && match(i, j+1))
		case b[j] == "*":
			result = match(i, j+1) || (absorbable(a[i]) && match(i+1, j))
		default:
			result = shareCharacter(a[i], b[j]) && match(i+1, j+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}
