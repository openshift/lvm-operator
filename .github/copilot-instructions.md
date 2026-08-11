# Derived from AGENTS.md — keep in sync

## Project

LVM Operator (LVMS) — Kubernetes operator managing LVM volume groups for OpenShift.
Single binary embedding TopoLVM CSI driver, optimized for edge/single-node clusters.

Tech stack: Go, controller-runtime, OLM, Ginkgo/Gomega, Kustomize.

## Build Commands

- `make build` — compile the operator binary
- `make test` — run unit tests
- `make docker-test` — run unit tests inside a Linux container (useful for non-Linux hosts)
- `make lint` — run linters
- `make verify` — formatting and generated file checks
- `make generate` — update deepcopy methods after API type changes
- `make manifests` — regenerate CRD YAML and RBAC after API type changes
- `make bundle` — regenerate OLM bundle manifests
- `make catalog` — regenerate OLM catalog
- `make e2e` — run end-to-end tests (requires live cluster)

## Boundaries

This operator manages physical storage. Mistakes destroy data.

**Always do:**
- Run `make generate && make manifests` after changing `api/v1alpha1/` types
- Run `make bundle && make catalog` after any change that affects CRDs, RBAC, config, or monitoring
- Run `make verify` before considering work complete
- Use pointer types (`*StructType`) for optional API fields
- Treat `nil` as "upgraded from before this field existed"

**Ask first:**
- Adding new dependencies to `go.mod`
- Modifying webhook validation logic
- Changing device selector behavior
- Any schema migration or breaking API change

**Never do:**
- Edit generated files: `zz_generated*.go`, `config/crd/bases/`, `vendor/`
- Run destructive LVM commands: `wipefs`, `vgremove`, `pvremove`, `lvremove`, `mkfs`, `dd`
- Skip finalizer logic — three-level hierarchy prevents orphaned storage
- Commit secrets, credentials, or personal registry references

## CRD Modification Workflow

1. Edit type definition in `api/v1alpha1/*_types.go`
2. Add kubebuilder markers for validation, defaults, documentation
3. Run `make generate` to update deepcopy methods
4. Run `make manifests` to regenerate CRD YAML and RBAC
5. Run `make bundle && make catalog` to regenerate OLM bundle and catalog
6. Update or add controller logic
7. Add unit tests for validation and controller behavior
8. Add e2e tests if user-facing workflow changes

## Key Directories

| Path | Contents |
|------|----------|
| `api/v1alpha1/` | CRD type definitions and webhook validation |
| `cmd/` | Binary entrypoints (operator and vgmanager subcommands) |
| `internal/controllers/` | Reconcilers: lvmcluster, vgmanager, persistent-volume, node removal |
| `internal/controllers/lvmcluster/resource/` | Resource managers (DaemonSet, StorageClass, CSIDriver, etc.) |
| `config/` | Kustomize overlays, CRD manifests, RBAC, samples |
| `test/e2e/` | End-to-end test suite |
| `docs/` | Architecture, design, conventions, ADRs |

## Commit Conventions

```text
component: short description

Body explaining why, not what.

Signed-off-by: Name <email>
Co-Authored-By: Claude <noreply@anthropic.com>
```

- Type/component must not be empty
- Max 72 characters per line
- No full stop in header
- Must be signed off (`-s`)

## Documentation

See [AGENTS.md](../AGENTS.md) for full agent guidance, documentation index, and deep links.
