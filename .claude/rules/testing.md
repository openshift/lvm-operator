---
paths:
  - "test/**"
  - "**/*_test.go"
description: Testing conventions for unit tests, integration tests, and E2E
---

## Test Naming

- Describe reconciliation outcomes, not CRUD: "reconcile succeeds on creation" not "create LVMCluster"
- Test names form readable sentences from Describe+It

## Unit Tests

- Fakeclient preferred over envtest for vgmanager tests
- All system command execution through an Executor interface
- Inject resolution functions alone, not full resolver objects
- Use `go:embed` for YAML templates, not per-instance files
- envtest has no garbage collection — verify ownerReferences, don't test cascade deletion

## E2E Tests

- `Eventually().WithContext(ctx).Should(Succeed())` for polling, not manual loops
- `By()` for readable steps
- `Consistently` for negative assertions (e.g., PVC stays Pending), not single check after sleep
- Validate in reconciliation order: LVMCluster → CSI Driver → CSINodeInfo → VG Manager → LVMVolumeGroup → StorageClass → VolumeSnapshotClass
- Pod images: busybox (not nginx/ubuntu). Test namespaces: `privileged` PSA enforcement
- Assert on specific rejection reasons, not just that creation failed

## Cleanup

- Tests must clean up: explicit cleanup steps in test body AND defers
- `deleteLVMClusterSafely` (strips finalizers) is acceptable in integration test defers as a safety net
- For non-defer test steps, use `deleteSpecifiedResource` (normal operator cleanup path)
- Stripping finalizers is never acceptable in controller/production code
- After deleting a retained PV, clean up the underlying LogicalVolume CR

## Build Integration

- After `api/v1alpha1/` changes: `make generate && make manifests && make bundle && make catalog`
- Split code changes and regenerated manifests into separate commits

Full conventions: [docs/conventions/testing.md](../../docs/conventions/testing.md)
