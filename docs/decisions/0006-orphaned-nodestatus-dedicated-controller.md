---
status: accepted
date: 2023-08-14
decision-makers: LVMS team (jakobmoellerdev)
consulted: LVMS team
informed:
---

# Dedicated Controller for Orphaned NodeStatus Cleanup

## Context and Problem Statement

When a node is removed from the cluster, its `LVMVolumeGroupNodeStatus` CR becomes orphaned. These stale status CRs accumulate and can confuse operators and agents reading cluster state. A mechanism was needed to clean them up.

## Decision Drivers

* Orphaned NodeStatus CRs must be removed without manual intervention.
* The cleanup mechanism must handle nodes that disappear without graceful shutdown.
* The approach must not break the existing reconciliation model.

## Considered Options

1. Clean up orphaned NodeStatus during LVMCluster status update — check all nodes on every status reconcile and delete stale entries
2. Dedicated node-removal controller with finalizer — watches node changes and cleans up on node deletion

## Decision Outcome

Chosen option: "Dedicated node-removal controller with finalizer", because it separates concerns cleanly and avoids expensive all-node comparisons on every status update. The tradeoff is that if the finalizer is not removed (e.g., controller bug), the Node object blocks deletion — but this is preferable to silent accumulation of stale status.

A stale-finalizer cleaner on LVMVolumeGroup removes finalizers for nodes that no longer exist, handling the edge case of nodes deleted while the operator was down.

### Consequences

* Good, because cleanup is event-driven (node deletion) rather than polling-based.
* Good, because the LVMCluster status update path stays simple.
* Bad, because a stuck finalizer on the Node object blocks node deletion until the operator fixes it.

## More Information

* [PR #372](https://github.com/openshift/lvm-operator/pull/372) — orphaned NodeStatus cleanup controller
* `internal/controllers/lvmcluster/controller.go` — stale finalizer cleaner (`checkStaleNodeFinalizers`)
