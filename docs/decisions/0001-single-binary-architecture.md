---
status: accepted
date: 2022-07-01
decision-makers: Bulat Zamalutdinov
consulted: LVMS team
informed: OpenShift storage team
---

# Single Binary Architecture for Edge Deployment

## Context and Problem Statement

LVMS depends on TopoLVM and several CSI sidecar components. The standard Kubernetes model runs each as separate containers. A decision was needed on how to package these for edge and single-node clusters where resources are constrained.

## Decision Drivers

* Edge/SNO clusters have limited CPU and memory.
* Startup time matters for edge recovery.
* Upgrade complexity scales with independently versioned components.

## Considered Options

1. **Multi-container deployment** — run TopoLVM controller, CSI sidecars, and the operator as separate Deployments/DaemonSets, each with its own container image.
2. **Single binary with embedded TopoLVM and CSI sidecars** — compile TopoLVM controllers directly into the operator and vg-manager binaries using Go's `replace` directive.

## Decision Outcome

Chosen option: "Single binary with embedded TopoLVM and CSI sidecars", because:

- Fewer pods reduce resource overhead on constrained edge nodes.
- Startup is faster with no inter-deployment dependency chain.
- A single binary simplifies upgrades and rollback — one image to build, test, and ship.
- Version skew between TopoLVM and the operator is eliminated at compile time.

### Consequences

* Good, because edge clusters use fewer resources and start faster.
* Good, because there is no version skew between operator and CSI driver.
* Bad, because upstream TopoLVM changes require a downstream fork (`github.com/openshift/topolvm`) with a `replace` directive, adding merge overhead.
* Bad, because debugging requires understanding that `cmd/main.go` multiplexes `operator` and `vgmanager` subcommands into one cobra root.

## More Information

* [docs/architecture.md](../architecture.md) — "Why LVMS Embeds TopoLVM" section
* [docs/upstream.md](../upstream.md) — upstream contribution workflow
* `cmd/main.go` — single cobra root with `operator` and `vgmanager` subcommands
