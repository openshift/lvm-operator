---
status: accepted
date: 2026-03-03
decision-makers: LVMS team (qJkee, Neilhamza)
consulted: LVMS team
informed: OpenShift Edge team, QE
---

# Server-Side Apply for StorageClassOptions

## Context and Problem Statement

LVMS needed to allow users to configure StorageClass properties (reclaimPolicy, volumeBindingMode, additional parameters, additional labels) via the LVMCluster CR. The challenge was managing field ownership: LVMS controls certain keys (device-class name, filesystem type, managed labels) while users provide additional parameters and labels. The two sets must not conflict, and day-2 changes to either must reconcile cleanly.

The previous StorageClass reconciliation used `CreateOrUpdate` with a mutate function, which overwrites the entire object and cannot distinguish between LVMS-owned and user-owned fields.

## Decision Drivers

* Users must be able to set additional StorageClass parameters and labels without LVMS overwriting them.
* LVMS must maintain ownership of critical parameters (`topolvm.io/device-class`, `csi.storage.k8s.io/fstype`) and managed labels — user values must never override these.
* Day-2 changes (modifying `AdditionalLabels`, toggling default SC annotation) must reconcile without requiring StorageClass deletion and recreation. Note: `AdditionalParameters` is immutable after creation (CEL); only `AdditionalLabels` can be changed day-2.
* Immutability of critical fields (reclaimPolicy, volumeBindingMode, fstype) must be enforced at the API level.

## Considered Options

1. `CreateOrUpdate` with mutate function (existing approach)
2. Kubernetes Server-Side Apply (SSA) with field ownership

## Decision Outcome

Chosen option: "Server-Side Apply with field ownership", because SSA's field manager model natively handles the LVMS-owned vs. user-owned key distinction, and day-2 annotation changes reconcile automatically without object recreation.

Implementation details:

* StorageClasses are patched with `client.Apply`, `client.FieldOwner("lvms-operator")`, and `client.ForceOwnership`.
* User-provided `AdditionalParameters` and `AdditionalLabels` are copied first, then LVMS-owned keys are set afterward to prevent user override (PR #2066: qJkee caught that naive `maps.Copy()` of user values would silently override managed keys).
* Immutability is enforced via CEL markers: `reclaimPolicy`, `volumeBindingMode`, and `fstype` use `XValidation: rule="oldSelf == self"`.
* A follow-up fix (PR #2494) addressed a CEL bypass where `+kubebuilder:default={}` was needed on the optional `StorageClassOptions` struct to ensure `oldSelf` always exists on UPDATE.

### Why additionalParameters Exists

`additionalParameters` is a forward-compatibility passthrough. TopoLVM only reads two StorageClass parameters (`topolvm.io/device-class` and `csi.storage.k8s.io/fstype`) — any other parameter is passed through to the CSI `CreateVolume` call but ignored. The use case is extensibility: if TopoLVM or a future CSI sidecar adds support for new parameters, customers can set them without waiting for an LVMS API change. Custom parameters landing on the StorageClass object are visible to any component that reads SC parameters.

Immutable fields (reclaimPolicy, volumeBindingMode, fstype) require delete-and-recreate to change — there is no patch mechanism by design.

**Known gap**: LVMS-owned keys passed via additionalParameters are silently overridden by the operator (user values copied first, then managed keys set). The webhook does not currently warn about this. Discussed but not yet implemented.

### Consequences

* Good, because SSA handles field ownership natively — LVMS-owned keys cannot be overridden by users.
* Good, because day-2 changes to the default SC annotation reconcile automatically.
* Good, because the SSA patch is declarative and idempotent, fitting the operator's reconciliation model.
* Good, because additionalParameters provides forward-compatibility without API changes.
* Bad, because SSA is more complex to reason about than `CreateOrUpdate` for contributors unfamiliar with field managers.
* Bad, because the webhook does not warn when users pass LVMS-owned keys via additionalParameters — they are silently overridden.

## More Information

* [PR #2066](https://github.com/openshift/lvm-operator/pull/2066) — original implementation
* [PR #2494](https://github.com/openshift/lvm-operator/pull/2494) — CEL immutability bypass fix
* `internal/controllers/lvmcluster/resource/topolvm_storageclass.go` — SSA patch and key ordering
* `api/v1alpha1/lvmcluster_types.go` — `StorageClassOptions` struct with immutability markers
