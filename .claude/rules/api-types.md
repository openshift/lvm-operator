---
paths:
  - "api/v1alpha1/**"
description: CRD modification workflow and validation conventions
---

## CRD Change Workflow

Use `/modify-crd` for the full step-by-step workflow. Never edit `zz_generated*.go` or `config/crd/bases/` directly.

## API Rules

- `v1alpha1` is the stable production API — treat as stable despite the name
- New fields must be optional (`+optional`, pointer type, `omitempty`)
- `nil` means "upgraded from before this field existed" — default to backward-compatible behavior
- Use `*StructType` for optional structs to enable nil-checking
- Required fields cannot have `omitempty`
- Status values must be typed constants, not bare strings

## Validation

- Safety-critical constraints: validate at CRD schema (markers), webhook, AND controller
- Non-safety validation: webhook only, don't duplicate in controller
- Validation functions must not mutate or cause side effects
- Objects in webhook handlers are guaranteed non-nil — no nil-check needed
- Use kubebuilder markers from the start for new config structures

## Gotcha: CEL XValidation Nil Bypass

Transition rules (`oldSelf == self`) are silently skipped when `oldSelf` does not exist (field added in upgrade). Fix: add `+kubebuilder:default={}` on optional struct fields with immutable sub-fields, plus a webhook guard for upgrades.

Full conventions: [docs/conventions/api-and-validation.md](../../docs/conventions/api-and-validation.md)
