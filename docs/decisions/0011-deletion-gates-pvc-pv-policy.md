---
status: accepted
date: 2026-03-03
decision-makers: LVMS team (qJkee, Neilhamza)
consulted: LVMS team
informed: OpenShift Edge team
---

# Deletion Gates: PVC and PV Policy Checks Before LVMCluster Deletion

## Context and Problem Statement

When an LVMCluster is deleted, LVMS must decide whether to proceed with VG cleanup or block. If PVCs still reference LVMS StorageClasses, deleting the VG underneath them silently destroys user data. If PVs with Retain policy exist, the user expects the data to survive — deleting the VG violates that contract.

## Decision Drivers

* Silent data loss on deletion is unacceptable.
* Retain-policy PVs explicitly signal the user wants data preserved.
* Large clusters may have thousands of PVCs — checking all of them is expensive.
* Device class removal on day-2 must follow the same safety rules as full cluster deletion.

## Considered Options

1. Delete everything unconditionally — fast but destroys user data without warning
2. Cascade via OwnerReferences — clean but OwnerReferences on cluster-scoped resources (StorageClass) don't work cross-namespace
3. Block deletion with explicit PVC/PV gates and emit events for manual cleanup

## Decision Outcome

Chosen option: "Block deletion with explicit PVC/PV gates", because data safety trumps deletion speed, and users must be told exactly what to clean up.

Implementation:

* LVMCluster finalizer checks for active PVCs using LVMS StorageClasses via `client.MatchingFields{"spec.storageClassName": scName}` with field indexers (never unfiltered `List()` — hangs in large clusters, PR #2066).
* Retain-policy PVs are checked separately. If user logical volumes exist with Retain policy, cleanup is blocked and a `ManualCleanupRequired` event is emitted.
* Device class removal on day-2 follows the same gates — users must delete PVCs first (PR #1657).
* `deleteSpecifiedResource` waits for the CR to disappear but not for VG cleanup on disk — `waitForLVMClusterReady` on the new cluster detects VG conflicts (PR #2410).

### Consequences

* Good, because data loss on deletion is prevented.
* Good, because the `ManualCleanupRequired` event tells users exactly what to do.
* Good, because field indexers make PVC/PV lookup fast even in large clusters.
* Bad, because deletion can block indefinitely if users don't clean up their PVCs/PVs.

## More Information

* [PR #2066](https://github.com/openshift/lvm-operator/pull/2066) — filtered PVC/PV listing with field indexers
* [PR #1657](https://github.com/openshift/lvm-operator/pull/1657) — device class removal on day-2
* [PR #402](https://github.com/openshift/lvm-operator/pull/402) — ownerref alignment and deletion logic
* `internal/controllers/lvmcluster/controller.go` — finalizer and PVC/PV gate logic
