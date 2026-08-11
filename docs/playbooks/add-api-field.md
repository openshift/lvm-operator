# Playbook: Add a New API Field End-to-End

For the step-by-step CRD modification workflow, use the `/modify-crd` command or see [.claude/commands/modify-crd.md](../../.claude/commands/modify-crd.md). For API type conventions, see [.claude/rules/api-types.md](../../.claude/rules/api-types.md).

This playbook covers what those files don't: LVMS-specific patterns for field propagation, mutual exclusivity, and testing that are unique to how this operator is structured.

## DeviceClass-to-LVMVolumeGroup propagation

Adding a field to `DeviceClass` does NOT automatically propagate it to `LVMVolumeGroupSpec`. You must:

1. Add the field to both `DeviceClass` (in `lvmcluster_types.go`) and `LVMVolumeGroupSpec` (in `lvmvolumegroup_types.go`) with identical markers
2. Add `YourField: deviceClass.YourField` to the `lvmVolumeGroups()` mapping in `internal/controllers/lvmcluster/resource/lvm_volumegroup.go`
3. Write a test verifying the field reaches `LVMVolumeGroupSpec`

## Mutual exclusivity pattern

Follow [ADR-0012](../decisions/0012-cel-vs-webhook-validation.md):

- **LVMCluster**: enforce in the webhook (`verifyYourField()` in `lvmcluster_webhook.go`). Add error sentinel (`ErrYourFieldAndOtherMutuallyExclusive`). Call in both `ValidateCreate` and `ValidateUpdate`.
- **LVMVolumeGroup**: enforce via CEL only (no webhook exists). Add `+kubebuilder:validation:XValidation:rule="!(has(self.fieldA) && has(self.fieldB))"` on `LVMVolumeGroupSpec`.

Do not add mutual exclusivity CEL on `DeviceClass` — it's webhook-enforced there.

## Testing checklist

- **Webhook tests** (`api/v1alpha1/lvmcluster_test.go`): valid create, invalid create (mutual exclusivity), invalid update (mutual exclusivity added on update), immutability rejection on update, mutable fields allowed on update. Use `defaultLVMClusterInUniqueNamespace(ctx)` helper.
- **LVMVolumeGroup CEL tests**: since LVMVolumeGroup has no webhook, verify CEL rules directly — test that mutually exclusive fields on `LVMVolumeGroupSpec` are rejected at admission time.
- **Propagation test**: verify the field flows from DeviceClass → LVMVolumeGroup via `lvmVolumeGroups()`.

## Common mistakes

- Assuming DeviceClass fields flow to LVMVolumeGroup automatically (they don't — explicit mapping required)
- Adding mutual exclusivity CEL on DeviceClass (webhook handles it there)
- Adding webhook validation for create but not update (or vice versa)
- Using `oldSelf == self` CEL on an optional struct without `+kubebuilder:default={}` (nil-bypass — see [.claude/rules/api-types.md](../../.claude/rules/api-types.md))
