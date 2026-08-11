# CRD Modification Workflow

Follow these steps when modifying CRD types. Do not skip steps.

## 1. Edit Type Definition

Edit the relevant type in `api/v1alpha1/*_types.go`:
- Add godoc comments for all new fields
- Use pointer types (`*StructType`) for optional structs
- New fields must be optional with `omitempty`
- `nil` means "upgraded from before this field existed"

## 2. Add Kubebuilder Markers

Add validation, default, and documentation markers:
```go
// +optional
// +kubebuilder:validation:Optional
FieldName *string `json:"fieldName,omitempty"`
```

For immutable fields, add CEL validation:
```go
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="fieldName is immutable"
```

**Gotcha**: CEL transition rules are skipped when `oldSelf` doesn't exist (new field on upgrade). Add `+kubebuilder:default={}` on optional structs with immutable sub-fields.

## 3. Regenerate

```bash
make generate    # updates zz_generated.deepcopy.go
make manifests   # regenerates config/crd/bases/ and RBAC
make bundle      # regenerates OLM bundle manifests
make catalog     # regenerates OLM catalog
```

Never edit generated files directly.

## 4. Update Controller Logic

Add handling for the new field in the relevant controller under `internal/controllers/`.

## 5. Add Webhook Validation

If the field needs cross-field validation, add it to the webhook in `api/v1alpha1/lvmcluster_webhook.go`. Safety-critical constraints must be validated at webhook AND controller.

## 6. Add Tests

- Unit test: validation logic and controller behavior
- E2E test: if the change affects user-facing workflows

## 7. Verify

```bash
make test        # unit tests pass
make verify      # formatting and generated files are clean
make lint        # no linter violations
```
