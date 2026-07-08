---
applyTo: "api/v1alpha1/**"
---
# Derived from AGENTS.md — keep in sync

## CRD Type Conventions

- `v1alpha1` is the stable production API — name is historical
- New fields must be optional: `+optional`, pointer type, `omitempty`
- `nil` means "upgraded from before this field existed" — default to backward-compatible behavior
- Use `*StructType` for optional structs to enable nil-checking
- Required fields cannot have `omitempty`

## After Editing Types

Always run:
```bash
make generate    # updates zz_generated.deepcopy.go
make manifests   # regenerates CRD YAML and RBAC
make bundle      # regenerates OLM bundle manifests
make catalog     # regenerates OLM catalog
```

Never edit `zz_generated*.go` or `config/crd/bases/` directly.

## Validation

- Safety-critical constraints: validate at CRD schema (markers), webhook, AND controller
- Use kubebuilder markers from the start for new config structures
- Validation functions must not mutate or cause side effects

## CEL XValidation Gotcha

Transition rules (`oldSelf == self`) are silently skipped when `oldSelf` doesn't exist. Fix: add `+kubebuilder:default={}` on optional struct fields with immutable sub-fields.
