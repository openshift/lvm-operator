---
status: "accepted"
date: 2026-10-22
decision-makers: LVMS maintainers
consulted:
informed:
---

# Glob patterns, not regular expressions, for device selectors

## Context and Problem Statement

Administrators configuring `deviceSelector.paths` on nodes with many disks do not want
to enumerate every device by name. The natural ask is a wildcard: "use all the disks
matching `/dev/nvme2*`".

The first implementation added a `regex:` prefix whose remainder was compiled as a Go
regular expression and matched against the kernel device name. This ADR records why
that was replaced before it shipped.

## Decision Drivers

* `deviceSelector` can feed `wipefs` via `forceWipeDevicesAndDestroyAllData`. A pattern
  that does not mean what the author thinks it means is a data-loss bug.
* Administrators must be able to check a pattern before applying it.
* LVMS already warns that kernel names are unstable and recommends `/dev/disk/by-id/…`,
  so the matching syntax should not add a second source of surprise on top of that.

## Considered Options

1. Go regular expressions behind a `regex:` prefix.
2. Shell-style globs (`path/filepath.Match`) behind a `glob:` prefix.
3. Both syntaxes, selected by prefix.

## Decision Outcome

Chosen option: "Shell-style globs", because the motivating example only works under
glob semantics, and because the failure modes of the two syntaxes are asymmetric in a
way that matters on a destructive path.

In a regular expression `*` is a quantifier binding to the preceding character; in a
glob it is a wildcard. Matched against real device names:

| pattern | as Go regexp | as `filepath.Match` |
|---|---|---|
| `/dev/nvme2*` | `/dev/nvme`, `/dev/nvme2`, `/dev/nvme22` | `/dev/nvme2n1`, `/dev/nvme2n1p1` |
| `/dev/sd[b-d]` | `/dev/sdb`, `/dev/sdc`, `/dev/sdd` | identical |

The intuitive spelling selects nothing under regular expressions, while the spelling
that works — `/dev/sd.*` — looks almost identical to one that matches nothing. The safe
looking pattern must not be the broken one when the next step may be `wipefs`.

Option 3 was rejected: supporting both prefixes means `/dev/nvme2*` silently selects
different devices depending on which prefix the author typed, which is the same
ambiguity with an extra way to reach it.

### Consequences

* Good, because `ls <pattern>` on the node previews the candidate paths, before the
  filter chain and the deduplication and ancestor/descendant rules narrow them.
* Good, because `*` and `?` never cross a `/`, so no pattern can escape its directory,
  and `.` is literal rather than "any character".
* Good, because character classes — `/dev/sd[b-d]`, `/dev/nvme[1-3]n1` — are unchanged
  between the two syntaxes, so the expressive cases in the original design still work.
* Bad, because alternation (`a|b`) is lost. `paths` is a list, so this costs one extra
  list entry.
* Bad, because patterns still match kernel names only and do not expand
  `/dev/disk/by-id/…` symlinks, which is the naming scheme the documentation otherwise
  recommends. Matching stable aliases is deliberately left for a follow-up.
* Bad, because patterns are not accepted alongside `raidConfig`. RAID requires an exact
  device count that admission cannot derive from a pattern, and RAID geometry is fixed at
  `lvcreate` time. Restricting now is reversible; restricting after release would not be.

## More Information

* Implementation: `api/v1alpha1/device_path.go`, `internal/controllers/vgmanager/device_paths.go`
* Overlap detection between two patterns: `internal/glob`. Deciding whether two globs can
  match a common name is a product-automaton walk over the two patterns, O(len(a)*len(b));
  testing each pattern against the other's text is not equivalent and misses real overlaps
  such as `/dev/sd*` against `/dev/*1`.
* Usage and semantics: [docs/design/vg-manager.md](../design/vg-manager.md)
