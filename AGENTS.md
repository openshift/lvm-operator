This file provides guidance to AI agents working with the LVM Operator repository. It is an index into deeper documentation — read the linked files for full details.

## Repo Orientation

This is the LVM Operator, part of LVMS (Logical Volume Manager Storage) for OpenShift. It manages LVM volume groups on cluster nodes via a Kubernetes operator and the TopoLVM CSI driver. The operator and TopoLVM are compiled into a single binary for edge/single-node efficiency.

- [README.md](README.md) — what LVMS is, deployment, known limitations
- [CONTRIBUTING.md](CONTRIBUTING.md) — build commands, testing, commit conventions, AI attribution
- [Official product documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/storage/configuring-persistent-storage#persistent-storage-using-lvms)

## Documentation Index

| Document | Purpose |
|----------|---------|
| [docs/core-beliefs.md](docs/core-beliefs.md) | Non-negotiable design principles — read this first |
| [docs/conventions/](docs/conventions/) | Implementation conventions enforced in review (split by area) |
| [docs/domain/glossary.md](docs/domain/glossary.md) | LVMS terminology — canonical definitions matching Go types and CRD fields |
| [docs/domain/concepts.md](docs/domain/concepts.md) | How LVMS concepts relate — resource flow, filter chain, reconciliation, deletion |
| [docs/decisions/](docs/decisions/index.md) | Architectural Decision Records (ADRs) — why the codebase looks the way it does |
| [docs/architecture.md](docs/architecture.md) | Design rationale, component diagram, reconciliation lifecycle, CRD relationships, finalizer hierarchy |
| [docs/design/lvm-operator-manager.md](docs/design/lvm-operator-manager.md) | LVM Operator Manager internals |
| [docs/design/vg-manager.md](docs/design/vg-manager.md) | Volume Group Manager design |
| [docs/design/thin-provisioning.md](docs/design/thin-provisioning.md) | Thin provisioning design |
| [docs/design/raid-support.md](docs/design/raid-support.md) | RAID support design |
| [docs/upstream.md](docs/upstream.md) | Upstream TopoLVM workflow, when to contribute upstream vs. downstream fork |
| [docs/dependency-management.md](docs/dependency-management.md) | Updating Go, Kubernetes, and TopoLVM dependencies |
| [docs/known-limitations.md](docs/known-limitations.md) | Device filters, RAID/encryption workarounds, snapshot constraints |
| [docs/loop-devices.md](docs/loop-devices.md) | Using loop devices for testing and development |
| [docs/security.md](docs/security.md) | Snyk vulnerability scanning |
| [docs/troubleshooting.md](docs/troubleshooting.md) | Troubleshooting guide |

## Modifying CRDs

When changing API types in `api/v1alpha1/`, follow this sequence:

1. Edit the type definition in `api/v1alpha1/*_types.go`.
2. Add kubebuilder markers for validation, defaults, and documentation.
3. Run `make generate` to update deepcopy methods.
4. Run `make manifests` to regenerate CRD YAML and RBAC manifests.
5. Update or add controller logic to handle the new field.
6. Add unit tests for validation and controller behavior.
7. Add e2e tests if the change affects user workflows.

### Validation Markers

Use kubebuilder markers to express validation constraints. All constraints must be documented in the field's godoc comment:

```go
// +optional
// +kubebuilder:validation:Optional
// +kubebuilder:validation:MinItems=1
Paths []string `json:"paths,omitempty"`
```

### API Version Policy

- Current production API: `v1alpha1` (the name is historical — treat as stable).
- New fields should be optional to maintain backward compatibility.
- Breaking changes require migration support and careful review.

## Safety Considerations

This operator manages physical storage and performs destructive LVM operations. See [docs/core-beliefs.md](docs/core-beliefs.md) for non-negotiable invariants and [docs/conventions/](docs/conventions/) for implementation patterns.

- **Data loss risk**: LVM operations can wipe disks. Verify device selectors carefully in tests.
- **Idempotency**: controllers must handle partial states and retries safely. VG Manager reconciles every 30 seconds.
- **Finalizers**: LVMS uses a three-level finalizer hierarchy to prevent orphaned storage. Never skip finalizer logic. See [docs/architecture.md](docs/architecture.md) for details.
- **Privileged operations**: VG Manager runs as a privileged DaemonSet and executes LVM commands (`vgcreate`, `vgextend`, `lvcreate`, `wipefs`) directly on nodes.

## Testing

Unit tests use Ginkgo/Gomega and are located alongside source files:

```go
var _ = Describe("LVMCluster Controller", func() {
    Context("When reconciling a new LVMCluster", func() {
        It("Should create the TopoLVM deployment", func() {
            // Test implementation
        })
    })
})
```

E2E tests are in `test/e2e/` and require a live cluster with available block devices. See [CONTRIBUTING.md](CONTRIBUTING.md) for build, deploy, and test commands. See [docs/conventions/testing.md](docs/conventions/testing.md) for testing conventions.

## Key Directories

| Path | Contents |
|------|----------|
| `api/v1alpha1/` | CRD type definitions and webhook validation |
| `cmd/` | Binary entrypoints (operator and vgmanager subcommands) |
| `internal/controllers/` | Reconcilers: lvmcluster, vgmanager, persistent-volume, node removal |
| `internal/controllers/lvmcluster/resource/` | Resource managers (DaemonSet, StorageClass, CSIDriver, etc.) |
| `config/` | Kustomize overlays, CRD manifests, RBAC, samples |
| `test/e2e/` | End-to-end test suite |
| `test/performance/` | Performance and stress test tooling |
| `hack/` | Vendor patches and build scripts |
| `release/` | Konflux build configuration and RPM lock files |
| `docs/` | Architecture, design, troubleshooting, upstream workflow |
