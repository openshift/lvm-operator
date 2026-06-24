---
status: accepted
date: 2026-06-09
decision-makers: LVMS team (qJkee, Neilhamza)
consulted: LVMS team
informed: OpenShift Edge team
---

# CEL XValidation vs Webhook for Field Validation

## Context and Problem Statement

LVMS needs to validate field constraints (immutability, cross-field rules, conditional logic). Kubernetes offers two mechanisms: CEL XValidation rules embedded in the CRD schema, and admission webhooks. The team needed a consistent policy on when to use which.

A CEL bypass was discovered (PR #2494) where transition rules (`oldSelf == self`) are silently skipped when the field doesn't exist on the old object — revealing that CEL alone is insufficient for some safety-critical validations.

## Decision Drivers

* CEL rules are evaluated by the API server with no extra deployment — they survive webhook deletion.
* Complex CEL expressions are hard to read and maintain.
* The webhook can be deleted — safety-critical constraints cannot rely solely on it.
* Some constraints (e.g., RAID mirrors-only-for-raid1/raid10) span multiple fields and are painful to express in CEL.

## Considered Options

1. All-CEL — every validation as XValidation markers on the CRD
2. All-webhook — centralize validation in the admission webhook
3. Hybrid — CEL for simple invariants, webhook for complex cross-field logic

## Decision Outcome

Chosen option: "Hybrid", because CEL readability degrades sharply for cross-field rules, and the webhook provides a single place for complex logic — but CEL provides a safety net when the webhook is deleted.

Rules:

* **CEL**: simple immutability (`oldSelf == self`), basic format validation, enum-like constraints. Add `+kubebuilder:default={}` on optional struct fields with immutable sub-fields to prevent the nil-bypass (PR #2494).
* **Webhook**: cross-field validation (RAID/ThinPool mutual exclusion, device count per RAID level, mirrors-only constraints), conditional logic, anything that would hurt readability as CEL.
* **Controller runtime**: defense-in-depth for safety-critical constraints — LVMVolumeGroup has no webhook, so vg-manager must enforce its own invariants.

### Consequences

* Good, because simple rules are enforced even without the webhook.
* Good, because complex cross-field logic stays readable and maintainable in Go.
* Bad, because contributors must decide which layer to use — three places to look for validation.
* Bad, because the CEL nil-bypass requires the `+kubebuilder:default={}` workaround on every optional immutable struct.

## More Information

* [PR #2549](https://github.com/openshift/lvm-operator/pull/2549) — qJkee: "writing such complex CEL rule will be hard to read and manage in the future"
* [PR #2494](https://github.com/openshift/lvm-operator/pull/2494) — CEL nil-to-non-nil bypass discovery and fix
* [PR #131](https://github.com/openshift/lvm-operator/pull/131) — first CRD validation discussion
* `api/v1alpha1/lvmcluster_webhook.go` — webhook validation
* `api/v1alpha1/lvmcluster_types.go` — CEL XValidation markers
