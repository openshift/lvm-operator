---
status: draft
date: Pre-2023
decision-makers: LVMS team
consulted:
informed:
---

# Conservative Device Discovery Filters

## Context and Problem Statement

VG Manager discovers block devices on each node and decides which are safe for LVM volume groups. A wrong decision destroys user data. The filter chain must balance discovering all legitimate devices against never touching a device with data the user wants to keep.

## Decision Drivers

* Data loss from incorrect selection is unrecoverable and can happen silently.
* False negatives (missing a device) are recoverable via DeviceSelector. False positives (wiping data) are catastrophic.
* `deviceMinAge` was later removed because time-based filtering gives false confidence.

## Considered Options

<!-- LVMS team: was a permissive-by-default approach (include all, exclude known-bad) ever considered? What drove the decision toward conservative filtering? -->

1. TBD — requires LVMS team input on what alternatives were discussed
2. Conservative filtering (exclude by default, include only known-safe patterns)

## Decision Outcome

Chosen option: conservative filtering. See [known-limitations.md](../known-limitations.md) for the full filter chain documentation.

## More Information

* [docs/known-limitations.md](../known-limitations.md) — user-facing filter documentation
* `internal/controllers/vgmanager/filter/filter.go` — filter chain implementation
