# Upstream Workflow: TopoLVM

LVMS depends on [TopoLVM](https://github.com/topolvm/topolvm), an upstream CSI driver for managing logical volumes on Kubernetes. This document describes how the lvm-operator project relates to upstream and how changes flow between the repositories.

## Repository Relationships

| Repository | Role |
|------------|------|
| [topolvm/topolvm](https://github.com/topolvm/topolvm) | Upstream CSI driver. Community-maintained. |
| [openshift/topolvm](https://github.com/openshift/topolvm) | Downstream fork. Carries OpenShift-specific patches when upstream fixes are not available in time. |
| [openshift/lvm-operator](https://github.com/openshift/lvm-operator) | This repo. Vendors TopoLVM via `go.mod` with a `replace` directive pointing to the downstream fork. |

## How TopoLVM is Consumed

TopoLVM is **vendored as a Go dependency**, not deployed as a separate workload. The lvm-operator and vg-manager binaries embed TopoLVM controllers directly, compiling them into the same binary. This is an intentional design choice to reduce pod count and startup time on edge/single-node clusters.

In `go.mod`, the upstream module is declared and then replaced with the downstream fork:

```
require github.com/topolvm/topolvm <upstream-version>
replace github.com/topolvm/topolvm => github.com/openshift/topolvm <downstream-version>
```

Both versions should be API-compatible. The downstream fork exists to carry urgent patches that cannot wait for an upstream release.

## When to Contribute Upstream vs. Downstream

- **Upstream first**: Bug fixes and features that are not OpenShift-specific should be contributed to [topolvm/topolvm](https://github.com/topolvm/topolvm). Once merged and released upstream, update the dependency in lvm-operator.
- **Downstream fork**: Use [openshift/topolvm](https://github.com/openshift/topolvm) only for critical fixes that need immediate remediation and cannot wait for an upstream release cycle. These patches should be upstreamed as soon as possible and the fork patch removed once the upstream release includes the fix.

## How CSI Sidecars Are Consumed

The same embedding pattern applies to the standard Kubernetes CSI sidecars. Rather than deploying separate sidecar containers alongside the driver, LVMS vendors them as Go libraries and compiles them directly into the operator and vg-manager binaries:

| Sidecar | Go module | Embedded in |
|---------|-----------|-------------|
| external-provisioner | `github.com/kubernetes-csi/external-provisioner/v5` | operator (`internal/csi/provisioner.go`) |
| external-resizer | `github.com/kubernetes-csi/external-resizer` | operator (`internal/csi/resizer.go`) |
| external-snapshotter | `github.com/kubernetes-csi/external-snapshotter/v8` | operator (`internal/csi/snapshotter.go`) |
| node-driver-registrar | — | vg-manager (`internal/csi/registrar.go`, implemented directly via kubelet plugin registration API) |

This keeps the pod count low on edge/single-node clusters, consistent with the TopoLVM embedding decision. These dependencies are updated the same way as any other Go dependency — see [dependency-management.md](dependency-management.md).

## Updating the TopoLVM Dependency

See [dependency-management.md](dependency-management.md) for the full dependency update procedure, including the TopoLVM-specific section on "Expected Replacement of TopoLVM."

The general flow:

1. Update the `require` version in `go.mod` to the new upstream release.
2. Update the `replace` directive to the corresponding downstream fork version (or remove the replace if no downstream patches are needed).
3. Run `make godeps-update docker-build` to vendor and verify.
4. Apply any necessary vendor patches (see `hack/` directory).
5. Run `make rpm-lock` to regenerate RPM lock files for Konflux.
