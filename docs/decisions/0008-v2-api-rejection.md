---
status: rejected
date: 2023-10-17
decision-makers: LVMS team (jakobmoellerdev)
consulted: LVMS team
informed:
---

# V2 API Proposal (Rejected)

## Context and Problem Statement

A v2 API was proposed to address limitations of the current v1alpha1 API. The proposal explored whether LVMCluster should remain the single user-facing CR or whether users should be able to create LVMVolumeGroup CRs directly, and whether the API should support multiple volume groups per device class.

## Decision Drivers

* The current API couples all device classes into a single LVMCluster CR — modifying one device class risks affecting others.
* Direct LVMVolumeGroup creation would eliminate the indirection but lose the pre-validation guarantee.
* Multiple TopoLVM instances can run but lvmd instances race on locks.

## Considered Options

1. V2 API — users create LVMVolumeGroup directly, DeviceClass maps to N volume groups, thin and thick VGs coexist
2. Keep v1alpha1 — LVMCluster remains the single entry point, operator creates child CRs

## Decision Outcome

Chosen option: "Keep v1alpha1", because the pre-validation benefit of the LVMCluster → LVMVolumeGroup → LVMVolumeGroupNodeStatus hierarchy outweighs the flexibility of direct VG creation. Without the operator-mediated layer, simultaneous CRs on hundreds of nodes could create inconsistent VG-to-node mappings and dangling PVs.

The proposal was explicitly rejected but merged as documentation for historical reference — team convention is to preserve rejected designs in the repo.

### Consequences

* Good, because the safety guarantees of operator-mediated CR creation are preserved.
* Good, because the rejected design is documented for future reference.
* Bad, because modifying a single device class still requires editing the full LVMCluster CR.

## More Information

* [PR #432](https://github.com/openshift/lvm-operator/pull/432) — v2 API proposal (merged as documentation, rejected for implementation)
* [PR #72](https://github.com/openshift/lvm-operator/pull/72), [PR #100](https://github.com/openshift/lvm-operator/pull/100) — original discussions on why LVMCluster is the single entry point
