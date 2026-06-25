# Code Style and Observability Conventions

These conventions were extracted from PR review threads and may not all reflect current practice — verify against current code when in doubt.

## Code Style

- Error log messages should start with `"failed to..."` (not consistently enforced — some use "could not" or context-prefix patterns). Errors should be wrapped with `fmt.Errorf("context: %w", err)` where possible (32 bare `return err` exist in production code). Use `errors.New()` for static error strings. (#12, #2494, #386)
- Go filenames are lowercase; use underscores for multi-word names (e.g., `wipe_devices.go`, `lv_attr.go`). Directory names use hyphens (`persistent-volume`, not `persistent_volume` or `pv`) to avoid LVM physical volume ambiguity. (#49, #356)
- Import ordering: Go stdlib → GitHub → k8s, blank lines between groups. controller-runtime zap logger only — no logrus. (#690, #49)
- Prefer small named functions over inlining. Functions without receiver state should be package-level, not methods. Prefer early-return over nesting. No function-local type definitions. Prefer stdlib `slices.Contains` over hand-rolled helpers. (#133, #643, #1380, #1823)
- Error as last return value. Function comments must end with a period (godot linter). Exported fields required when tests live in a separate `_test` package. (#130, #369)
- Check `internal/controllers/constants/constants.go` and `api/v1alpha1/` before defining new constants. Reuse CSI driver name constants across StorageClass and VolumeSnapshotClass. (#2066, #183)
- Typed event-reason constants should not repeat the type name as prefix. (#403)
- Avoid package-level variables — prefer function receivers or passed contexts. (#229)
- Webhook error messages should not be prefixed with `"Error:"` — the framework already conveys it. (#349)
- Copyright header: "Copyright © 20XX Red Hat, Inc." (uses the Unicode © symbol, not `(c)`). CRD YAML must exactly match upstream TopoLVM definitions and begin with `---`. (#248, #224)
- V(2) log level was broken with `--zap-log-level=debug` as of PR #631 — V(1) was used for debug logs instead. This may have been fixed in newer controller-runtime versions. (#631, #1150)

## Observability

- Static YAML for alert rules over jsonnet/mixin. All alerts include VG name (device_class label) and node name. RAID alerts: one per deviceClass, not per PVC. (#103, #2337)
- Metrics through kube-rbac-proxy over TLS, not separate HTTP endpoints. `diagnostics-address` naming aligns with cluster-api convention. (#425, #431)
- Avoid redundant error logging at call site AND execution site. Include resource identifiers (name, SCC name, pod name) in log messages. Detailed logging: no duplicate fields, uniform capitalization. (#137)
- Must-gather: direct LVM commands (`lvs`, not `lvm lvs` — bare `lvm` opens interactive shell). `-a` flag for thin pool metadata. `oc debug` with script files and `-q` flag. VolumeSnapshots in PVC namespace, not operator namespace. Discover install namespace from CSV. Upload actual must-gather output for reviewer verification. (#142, #145, #1843)
- Event messages must include actionable diagnostics — per-node free storage, safe units. (#356)
- Namespace changes require upgrade testing validation before merge. Consider VolumeGroup status on deletion failure — failure status should reflect whether the requested operation succeeded. (#955, #1380)
