# LVMS Architectural Decision Records

ADRs document key design decisions with context, alternatives considered, and consequences. Foundational architecture is documented in [architecture.md](../architecture.md). Implementation conventions are in [conventions/](../conventions/).

Format: [MADR 4.0](https://adr.github.io/madr/) short template. See [adr-template.md](adr-template.md).

## Decision Log

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [0001](0001-single-binary-architecture.md) | Single binary architecture for edge deployment | Accepted | 2022-07-01 |
| [0002](0002-v1alpha1-is-stable.md) | v1alpha1 as stable production API | Accepted | 2022-07-01 |
| [0003](0003-finalizer-hierarchy.md) | Three-level finalizer hierarchy for cleanup safety | Accepted | 2022-07-01 |
| [0004](0004-conservative-device-filters.md) | Conservative device discovery filters | Accepted | 2022-07-01 |
| [0005](0005-remove-device-min-age.md) | Remove deviceMinAge time-based filtering | Accepted | 2023-08-10 |
| [0006](0006-orphaned-nodestatus-dedicated-controller.md) | Dedicated controller for orphaned NodeStatus cleanup | Accepted | 2023-08-14 |
| [0007](0007-no-errgroup-for-concurrent-reconciliation.md) | No errgroup for concurrent reconciliation | Accepted | 2023-08-23 |
| [0008](0008-v2-api-rejection.md) | V2 API proposal | Rejected | 2023-10-17 |
| [0009](0009-configmap-for-lvmd-config.md) | ConfigMap for lvmd configuration | Superseded | 2023-11-09 |
| [0010](0010-server-side-apply-for-storageclassoptions.md) | Server-side apply for StorageClassOptions | Accepted | 2026-03-03 |
| [0011](0011-deletion-gates-pvc-pv-policy.md) | Deletion gates: PVC and PV policy checks | Accepted | 2026-03-03 |
| [0012](0012-cel-vs-webhook-validation.md) | CEL XValidation vs webhook for validation | Accepted | 2026-06-09 |
| [0013](0013-konflux-final-pipelines-for-release-automation.md) | Konflux final pipelines for release automation | Proposed | 2026-07-20 |

## Adding a New ADR

1. Copy [adr-template.md](adr-template.md).
2. Name it `NNNN-short-slug.md` (next sequential number).
3. Fill in status, date, and decision-makers in the YAML frontmatter.
4. Add an entry to the table above.
