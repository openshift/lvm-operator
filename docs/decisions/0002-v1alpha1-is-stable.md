---
status: accepted
date: 2022-07-01
decision-makers: Bulat Zamalutdinov
consulted: LVMS team
informed: OpenShift storage team
---

# v1alpha1 as Stable Production API

## Context and Problem Statement

LVMS ships its CRDs under `lvm.topolvm.io/v1alpha1`. The name suggests instability, but the API has been production-stable for multiple releases. The question is whether to promote the version.

## Decision Drivers

* Migration cost: conversion webhooks, storage version migration, coordinated rollout.
* The API is effectively stable — immutability enforced via CEL markers.
* No customer or engineering request has justified the cost.

## Considered Options

1. **Promote to v1beta1 or v1** — introduce a new API version with a conversion webhook, run both versions during a migration window, then deprecate v1alpha1.
2. **Introduce a v2 API** — redesign the API surface (formally proposed and rejected in ADR 0008).
3. **Keep `v1alpha1` and treat as stable by convention** — document that the name is historical, enforce stability via CEL immutability markers and webhook validation.

## Decision Outcome

Chosen option: "Keep `v1alpha1` and treat as stable by convention", because:

- Migration to v1beta1/v1 requires conversion webhooks, storage version migration, and coordinated rollout across upstream TopoLVM and downstream OpenShift — cost not justified by any concrete need.
- The v2 API proposal was formally rejected (ADR 0008) after analysis showed the redesign offered no user-facing benefit proportional to the migration effort.
- CEL XValidation markers and webhook validation already enforce the immutability guarantees that a "stable" version label would imply.

### Consequences

* Good, because no migration effort is imposed on existing clusters.
* Good, because immutability is enforced mechanically (CEL markers), not by version-label convention.
* Bad, because the `v1alpha1` name confuses new contributors who expect instability.
* Bad, because if a breaking change is ever needed, the migration path is more complex than if a versioning pattern had been established early.

## More Information

* [docs/architecture.md](../architecture.md) — "API Version: v1alpha1" section
* [ADR 0008](0008-v2-api-rejection.md) — formal rejection of v2 API proposal
* `api/v1alpha1/lvmcluster_types.go` — immutability markers on production fields
