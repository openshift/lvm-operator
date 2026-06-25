---
status: accepted
date: 2023-08-23
decision-makers: LVMS team
consulted: LVMS team
informed:
---

# No errgroup for Concurrent Reconciliation

## Context and Problem Statement

The LVMCluster controller creates multiple subordinate resources concurrently. When one creation fails, the question is whether to cancel all in-flight operations or let them all attempt and aggregate errors.

## Decision Drivers

* Resource creation is idempotent — a failed attempt doesn't corrupt state.
* Cancelling successful in-flight operations wastes work and forces a full retry next cycle.
* A critical deadlock bug was discovered when using errgroup: if `ensureCreated` returned nil, the context was never cancelled, blocking the group indefinitely.

## Considered Options

1. `golang.org/x/sync/errgroup` — cancels context on first error
2. Raw goroutines with results channel and `errors.Join`

## Decision Outcome

Chosen option: "Raw goroutines with `errors.Join`", because errgroup's cancel-on-first-error semantic is wrong for resource creation where you want all operations to attempt regardless of individual failures.

### Consequences

* Good, because all resources are attempted even when some fail — reducing reconcile cycles to converge.
* Good, because the deadlock bug from errgroup's context cancellation is eliminated.
* Bad, because raw goroutine management requires more boilerplate than errgroup.

## More Information

* [PR #391](https://github.com/openshift/lvm-operator/pull/391) — concurrent apply/status checks, deadlock discovery
