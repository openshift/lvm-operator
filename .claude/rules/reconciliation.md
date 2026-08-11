---
paths:
  - "internal/controllers/**"
description: Reconciliation patterns, ownership, and finalizer conventions
---

## MutateFn Convention

- Construct desired resource state in the constructor
- Use MutateFn to set mutable fields updated on both create and update paths
- For immutable resources (CSIDriver): MutateFn body is `return nil`

## Ownership and Finalizers

- Three-level finalizer hierarchy: LVMCluster → LVMVolumeGroup → LVMVolumeGroupNodeStatus
- Cluster-scoped resources use labels for ownership (OwnerReferences don't work cross-namespace)
- `Delete` only sets `DeletionTimestamp` — finalizers gate actual deletion
- Non-blocking delete + finalizer removal is the correct teardown pattern (`--wait=true` deadlocks)
- Resource deletion must be idempotent

## Error Handling

- Don't use `errgroup` — it cancels context on first error. Use raw goroutines + `errors.Join`
- Don't fail-fast on multi-node operations — aggregate errors, continue for healthy nodes
- Don't check `errors.IsAlreadyExists` after `CreateOrUpdate` — that error is never returned
- Rely on Kubernetes optimistic concurrency for concurrent status updates

## Status and Conditions

- Use upstream `meta/v1.Condition` and `meta.SetStatusCondition`/`meta.FindStatusCondition`
- Initialize conditions at creation, then modify — don't recompute from scratch every reconcile
- Recompute state from conditions, not only on failure. Add Unknown as default for in-progress
- LVMCluster Ready: only set when all expected VGs are reconciled. Failed > Degraded > Ready

## RBAC

- RBAC must include `watch`+`list` verbs — controller-runtime informer cache requires them
- Separate service accounts per workload with minimal RBAC

Full conventions: [docs/conventions/reconciliation.md](../../docs/conventions/reconciliation.md)
