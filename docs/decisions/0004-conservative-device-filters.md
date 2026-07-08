---
status: accepted
date: 2022-07-01
decision-makers: Bulat Zamalutdinov
consulted: LVMS team
informed: OpenShift storage team
---

# Conservative Device Discovery Filters

## Context and Problem Statement

VG Manager discovers block devices on each node and decides which are safe for LVM volume groups. A wrong decision destroys user data. The filter chain must balance discovering all legitimate devices against never touching a device with data the user wants to keep.

## Decision Drivers

* Data loss from incorrect selection is unrecoverable and can happen silently.
* False negatives (missing a device) are recoverable via DeviceSelector. False positives (wiping data) are catastrophic.
* `deviceMinAge` was later removed (ADR 0005) because time-based filtering gives false confidence.

## Considered Options

1. **Permissive-by-default (include all, exclude known-bad)** — start with all block devices visible to lsblk, then filter out devices matching known-bad patterns (mounted filesystems, partitions, read-only, already in a VG). Unknown devices are included.
2. **Conservative filtering (exclude by default, include only known-safe)** — start with no devices, then include only devices that pass all filters in a chain: not mounted, not read-only, no filesystem signature, no partitions, not in use by Kubernetes, not already in a non-LVMS VG.

## Decision Outcome

Chosen option: "Conservative filtering", because:

- With physical storage, the cost asymmetry is extreme: wiping a disk with user data is catastrophic and unrecoverable, while missing a valid device is easily fixed by adding it to DeviceSelector.
- A permissive default would require maintaining an exhaustive blocklist of every possible "bad" device pattern across all hardware and OS versions — an open-ended problem.
- Conservative filtering makes the safe path the default path — operators who want LVMS to consume all devices opt in explicitly via nil DeviceSelector (greedy mode), which is itself a permanent, documented commitment.

### Consequences

* Good, because the default is safe — new contributors and agents cannot accidentally wipe data.
* Good, because the filter chain is auditable — each filter has a clear, testable predicate.
* Bad, because users with unusual device layouts may need to explicitly configure DeviceSelector, adding setup friction.
* Bad, because the filter chain has accumulated complexity (no bind mount filter exists; some filters are noted as design debt from PR #147).

## More Information

* [docs/known-limitations.md](../known-limitations.md) — user-facing filter documentation
* [ADR 0005](0005-remove-device-min-age.md) — removal of time-based filtering
* `internal/controllers/vgmanager/filter/filter.go` — filter chain implementation
