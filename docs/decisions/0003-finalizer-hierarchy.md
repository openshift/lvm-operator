---
status: accepted
date: 2022-07-01
decision-makers: Bulat Zamalutdinov
consulted: LVMS team
informed: OpenShift storage team
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

1. **Single finalizer on LVMCluster with controller-driven cleanup loop** — one finalizer on the top-level CR; the LVMCluster controller orchestrates cleanup by directly issuing LVM commands to each node (via Jobs or exec) and removing the finalizer when all nodes report clean.
2. **Three-level finalizer hierarchy** — separate finalizers on LVMCluster, LVMVolumeGroup, and LVMVolumeGroupNodeStatus, each controller removing its own finalizer when its level of cleanup is complete.

## Decision Outcome

Chosen option: "Three-level finalizer hierarchy", because:

- A single-finalizer approach requires the central controller to have direct knowledge of every node's LVM state and the ability to execute commands on each node — violating the DaemonSet model where VG Manager owns node-local operations.
- Per-node finalizers on LVMVolumeGroup (`cleanup.vgmanager.node.topolvm.io/{nodeName}`) let each VG Manager pod handle its own node's cleanup independently, naturally handling unreachable nodes (finalizer stays until the node comes back).
- The LVMCluster finalizer blocks until PVCs are removed and Retain-policy PVs are cleaned up, emitting `ManualCleanupRequired` events — preventing silent data loss.

### Consequences

* Good, because cleanup is distributed — each node handles its own state without central coordination.
* Good, because unreachable nodes don't block other nodes' cleanup — their finalizers remain until they recover.
* Good, because Retain-policy PVs get explicit `ManualCleanupRequired` events instead of silent deletion.
* Bad, because the three-level hierarchy is complex to reason about and debug when finalizers get stuck.
* Bad, because orphaned LVMVolumeGroupNodeStatus resources required a dedicated cleanup controller (ADR 0006).

## More Information

* [docs/architecture.md](../architecture.md) — "Cleanup and Finalizer Hierarchy" section
* [ADR 0006](0006-orphaned-nodestatus-dedicated-controller.md) — dedicated controller for orphaned NodeStatus
* `internal/controllers/lvmcluster/controller.go` — LVMCluster finalizer
* `internal/controllers/vgmanager/controller.go` — per-node cleanup finalizer
