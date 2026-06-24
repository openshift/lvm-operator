# Testing and Build Conventions

These conventions were extracted from PR review threads and may not all reflect current practice — verify against current code when in doubt.

## Testing

- Test descriptions describe reconciliation outcomes, not CRUD ("reconcile succeeds on creation", not "create LVMCluster"). Test names form readable sentences from Describe+It. Strip author tags. (#17, #1870, #2271)
- E2E: `Eventually().WithContext(ctx).Should(Succeed())` instead of manual polling. `By()` for readable steps. Current timeouts are 4min/1s (`test/e2e/validation_test.go`). Validate in reconciliation order: LVMCluster → CSI Driver → CSINodeInfo → VG Manager → LVMVolumeGroup → StorageClass → VolumeSnapshotClass. DaemonSet readiness checks compare `DesiredNumberScheduled` vs `NumberReady` accounting for `maxUnavailable`. (#378, #149)
- Integration tests use `oc` CLI, not Go API client, to mirror user patterns. `echo $(command)` is intentional in test utilities — collapses multi-line jq output for `strings.Fields()`. (#2383, #2294)
- `Consistently` for negative assertions (e.g., PVC stays Pending), not `Eventually` or single check after sleep. Assert on specific rejection reasons, not just that creation failed. (#2383, #2410)
- Tests must clean up. After deleting a retained PV, clean up the underlying LogicalVolume CR. Explicit cleanup steps in test body AND defers — defers handle early failure, explicit steps verify restored state. `deleteLVMClusterSafely` (which strips finalizers) is intentionally used in integration test defers as a safety net when LVMCluster gets stuck — this is the established pattern from openshift-test-private. For non-defer test steps, prefer `deleteSpecifiedResource` which uses the normal operator cleanup path. Stripping finalizers is never acceptable in controller/production code. (#130, #2383, #2416, #2260)
- Fakeclient was preferred over envtest for vgmanager tests as of PR #426. Inject the resolution function alone, not full resolver objects. go:embed YAML templates, not per-instance files. Reuse test fixtures. All system command execution through an Executor interface. (#426, #690, #237, #252, #59)
- E2E pod images: busybox (not nginx/ubuntu). Test namespaces: `privileged` PSA enforcement. VolumeSnapshotClass CRD is a cluster prerequisite — unit tests need a disable flag. (#401, #278, #183)
- Unit tests require Linux (LVM commands, loop devices). Non-blocking delete + finalizer removal is the correct teardown pattern; `--wait=true` deadlocks. (#2262, #2346, #2271)
- envtest has no garbage collection — verify ownerReferences instead of testing cascade deletion. SCC creation IS testable in envtest (confirmed in `controller_integration_test.go`). (#17, #18, #26)
- Cross-PR review consolidation: team reviews related PRs together and centralizes shared patterns. CI override (`/override`) accepted for known-flaky pipelines on critical bugfixes. Performance tests require coordinated openshift/release config changes. (#2410, #369, #445)

## Build System and CI

- Split code changes and regenerated manifests into separate commits. Keep PRs focused. After `api/v1alpha1/` changes: `make generate && make manifests && make bundle` and commit the diff. Bundle manifest changes must be audited line-by-line for unintended drift. (#18, #187, #2494, #283)
- All commits signed (`-s`) + precommit hooks, regardless of change size. Document upstream dependency changes that cause code removals, with links. (#1843, #727)
- Runtime pod introspection (`getRunningPodImage`) over env vars for container image. Cache the image string in the reconciler struct. (#52, #94)
- Pin operator-sdk version in Makefile. Download checks if binary exists first. (#230)
- Generated files (kustomization.yaml namespace) should be committed, not .gitignored. (#167)
- CI should fail fast on auth — move login before expensive build steps. (#290)
- Pin base image versions in Dockerfiles for external images. Exception: bundle manifests use `:latest` tags as placeholders — the render-templates script substitutes actual versions at build time. Current base images: main Dockerfile uses `fedora:latest`, must-gather uses `origin-cli`. (#533, #19, #305, #1341)
- Makefile has cross-platform `sed` handling (separate macOS/Linux paths). Docker builds use buildx platform args. (#392, #442)
- When bumping k8s dependencies, update all CSI sidecars simultaneously. Go version in `go.mod` must match the development stream. (#1898, #1123)
- Build targets must declare tool dependencies for clean environments. (#144)
- Verify PRs trigger correct Konflux/CI jobs for the target version. (#2038)
- Release-branch files should reference main branch canonical sources, not duplicate content. (#1823)
