---
status: draft
date: Pre-2023
decision-makers: LVMS team
consulted:
informed:
---

# Single Binary Architecture for Edge Deployment

## Context and Problem Statement

LVMS depends on TopoLVM and several CSI sidecar components. The standard Kubernetes model runs each as separate containers. A decision was needed on how to package these for edge and single-node clusters where resources are constrained.

## Decision Drivers

* Edge/SNO clusters have limited CPU and memory.
* Startup time matters for edge recovery.
* Upgrade complexity scales with independently versioned components.

## Considered Options

<!-- LVMS team: please fill in the alternatives that were discussed when this decision was made. Was multi-container deployment ever seriously evaluated? What made the team rule it out? -->

1. TBD — requires LVMS team input on what alternatives were discussed
2. Single binary with embedded TopoLVM and CSI sidecars

## Decision Outcome

Chosen option: single binary. See [architecture.md](../architecture.md) for full rationale.

## More Information

* [docs/architecture.md](../architecture.md) — "Why LVMS Embeds TopoLVM" section
* `cmd/main.go` — single cobra root with `operator` and `vgmanager` subcommands
