---
status: draft
date: Pre-2023
decision-makers: LVMS team
consulted:
informed:
---

# v1alpha1 as Stable Production API

## Context and Problem Statement

LVMS ships its CRDs under `lvm.topolvm.io/v1alpha1`. The name suggests instability, but the API has been production-stable for multiple releases. The question is whether to promote the version.

## Decision Drivers

* Migration cost: conversion webhooks, storage version migration, coordinated rollout.
* The API is effectively stable — immutability enforced via CEL markers.
* No customer or engineering request has justified the cost.

## Considered Options

<!-- LVMS team: was v1beta1 or v1 ever concretely proposed? What specifically killed the idea — just cost, or other factors? -->

1. TBD — requires LVMS team input on what alternatives were discussed
2. Keep `v1alpha1` and treat as stable by convention

## Decision Outcome

Chosen option: keep `v1alpha1`. See [architecture.md](../architecture.md) for full rationale.

## More Information

* [docs/architecture.md](../architecture.md) — "API Version: v1alpha1" section
* `api/v1alpha1/lvmcluster_types.go` — immutability markers on production fields
