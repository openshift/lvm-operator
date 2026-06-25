# API Design and Validation Conventions

For design principles, see [core-beliefs.md](../core-beliefs.md). For architecture, see [architecture.md](../architecture.md). These conventions were extracted from PR review threads and may not all reflect current practice — verify against current code when in doubt.

## API Design

- Use "DeviceClass" in user-facing API types, "VG" in internal code. DeviceClass names must follow DNS-1123 label format since they flow into StorageClass names. (#76, #45, #47)
- Use pointer types (`*ThinPoolConfig`) for optional structs — enables nil-checking. Required fields cannot have `omitempty`. (#143)
- API field defaults belong in kubebuilder markers on the type, not in controller logic. (#728)
- New optional fields: `nil` means "upgraded from before this field existed" — runtime must default to backward-compatible behavior. (#2038)
- Status values should be typed constants (enums), not bare strings. (#72)
- `LVMVolumeGroup.Status.Node` represents intent — includes nodes where VG creation was attempted but failed, not just successes. (#71)
- Use upstream `meta/v1.Condition` and `meta.SetStatusCondition`/`meta.FindStatusCondition` helpers. Initialize conditions at creation, then modify — don't recompute from scratch every reconcile. Status condition messages are capitalized sentences. (#628)
- LVMCluster Ready status must only be set when all expected VGs are reconciled. Hierarchy: Failed > Degraded > Ready. (#262)
- Do not add labels that assume a single-instance CR — could break in multi-CR scenario. (#34)
- Auto-generated StorageClass names are intentional for 1-click install. StorageClass name changes are breaking — K8s requires delete-and-recreate. (#101, #114)
- One thin pool per device class. Thin-pools have a 1:1 relationship with VGs. Multiple TopoLVM instances can coexist but lvmd instances race on locks. (#143, #432)
- Device paths: any `/dev/` prefix is accepted by the webhook. Official docs recommend stable naming (`/dev/disk/by-path/` or `/dev/disk/by-id/`) over symbolic names (`/dev/sdX`) which may change across reboots. Status reports resolved PV names from LVM, not user-specified paths. Device path overlap validation must consider nodeSelector. (#229)
- Nil DeviceSelector (dynamic discovery) is single-deviceClass only; multi-deviceClass requires explicit DeviceSelector. (#615)
- Rejected designs are documented in the repo for historical reference (v2 API proposal: #432).
- Feature annotations in CSV must be version-accurate per release branch. (#602)
- Resource manager name constants are distinct from Kubernetes resource names — renaming a resource doesn't require changing the manager's identifier. (#114)
- Consolidate redundant namespace env vars when functionally identical. (#56)
- User-visible default changes (e.g., making LVMS StorageClass the default) need documentation. (#210)

## Validation

- Safety-critical constraints must be validated at multiple layers (webhook + controller runtime) — see [core-beliefs.md](../core-beliefs.md). For non-safety validation (field format, optional defaults), avoid duplicating between webhook and controller. Webhook validation functions should not be exported unless the controller needs them. (#229, #2549)
- ThinPool SizePercent: `default=90, min=10, max=100`. Required fields cannot have `omitempty`. (#131)
- Validation functions should only validate — never perform mutations or side effects. (#728)
- Move non-safety validation checks (field format, optional defaults, duplicate-path detection) into admission webhooks, not the reconcile loop. Safety-critical constraints must remain in both — see line above. (#426)
- Cannot delete the default device class or the last device class. (#1657)
- Missing capacity annotations should be skipped (continue), not treated as errors. Use per-device-class capacity annotation (`capacity.topolvm.io/<deviceClass>`). (#385)
- Objects in webhook handlers are guaranteed non-nil by the API server — no nil-check needed. (#2339)
- Default DeviceClass validation must handle three distinct cases: zero, one, and multiple defaults. A single DeviceClass is implicitly default. (#307)
- Webhook update validation can block users from correcting wrong device paths — manual workaround required (scale down operator, edit CR). (#255)
- Use kubebuilder CRD validation markers from the start when introducing new config structures. (#131)
- Don't check `errors.IsAlreadyExists` after `CreateOrUpdate` calls — the error is never returned. (#139)

## Gotchas

- **CEL XValidation nil-to-non-nil bypass**: transition rules (`oldSelf == self`) are silently skipped when `oldSelf` does not exist. Fix: add `+kubebuilder:default={}` on optional struct fields with immutable sub-fields, plus a webhook guard for upgrades. (#2494)
- **Managed field merge order**: when merging user-provided parameters/labels with operator-managed ones, copy user values first, then overwrite with operator-managed keys. Naive `maps.Copy()` lets users override LVMS-owned keys. (#2066)
- **Never list all PVCs/PVs cluster-wide**: use `client.MatchingFields{"spec.storageClassName": scName}` with field indexers. Unfiltered listing hangs in large clusters. (#2066)
- **Multi-node readiness bug** (historical, fixed): `expectedVGCount` double-counting caused LVMCluster to stay "Progressing" when all nodes were Ready. The readiness logic was rewritten to use `validateDeviceClassSetup` which iterates per-device-class per-valid-node. (#383)
- **Reducing overprovisionRatio below already-consumed capacity**: unclear upstream TopoLVM behavior — may not be properly enforced. (#708)
