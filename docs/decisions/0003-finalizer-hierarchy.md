---
status: draft
date: Pre-2023
decision-makers: LVMS team
consulted:
informed:
---

# Three-Level Finalizer Hierarchy for Cleanup Safety

## Context and Problem Statement

When an LVMCluster is deleted, LVMS must clean up LVM resources on every node. If cleanup is incomplete, storage is orphaned with no API-level recovery. A decision was needed on how to coordinate distributed cleanup.

## Decision Drivers

* Storage orphaning is unrecoverable without SSH access to nodes.
* Nodes may be unreachable during deletion.
* Cleanup has ordering requirements: LVs before VGs before PVs.
* Retain-policy PVs should not be silently deleted.

## Considered Options

<!-- LVMS team: was a single finalizer on LVMCluster with a controller-driven cleanup loop considered? What problems did that approach have that led to the three-level design? -->

1. TBD — requires LVMS team input on what alternatives were discussed
2. Three-level finalizer hierarchy (LVMCluster → LVMVolumeGroup → LVMVolumeGroupNodeStatus)

## Decision Outcome

Chosen option: three-level hierarchy. See [architecture.md](../architecture.md) for full rationale.

## More Information

* [docs/architecture.md](../architecture.md) — "Cleanup and Finalizer Hierarchy" section
* `internal/controllers/lvmcluster/controller.go` — LVMCluster finalizer
* `internal/controllers/vgmanager/controller.go` — per-node cleanup finalizer
