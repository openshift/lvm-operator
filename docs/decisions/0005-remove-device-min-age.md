---
status: accepted
date: 2023-08-10
decision-makers: LVMS team
consulted: LVMS team
informed:
---

# Remove deviceMinAge Time-Based Device Filtering

## Context and Problem Statement

LVMS had a `deviceMinAge` parameter that required devices to exist for a minimum duration before being claimed for a volume group. The intent was to prevent LVMS from grabbing a device that another system was about to claim.

## Decision Drivers

* The race condition it tries to prevent is fundamentally unsolvable with time: device attached at t0, LVMS checks at t1, external entity doesn't claim within the window, LVMS grabs it anyway.
* LVM's own system lock prevents concurrent LVM calls, making the age check redundant for LVM-level races.
* Time-based guards give users false confidence that their devices are protected.

## Considered Options

1. Keep `deviceMinAge` with a longer default window
2. Remove `deviceMinAge` entirely — rely on DeviceSelector for explicit device management

## Decision Outcome

Chosen option: "Remove `deviceMinAge` entirely", because time-based filtering is fundamentally unreliable and gives false confidence. Users who need control over which devices LVMS claims should use explicit `DeviceSelector` paths.

### Consequences

* Good, because false-confidence safety mechanism is removed — users must make explicit choices.
* Good, because reconciliation is simplified with one fewer timing-dependent check.
* Bad, because users relying on the delay for ad-hoc device management lose that (unsound) safety net.

## More Information

* [PR #380](https://github.com/openshift/lvm-operator/pull/380) — deviceMinAge removal
