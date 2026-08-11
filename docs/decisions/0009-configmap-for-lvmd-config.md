---
status: superseded
date: 2023-11-09
decision-makers: LVMS team (jakobmoellerdev)
consulted: LVMS team
informed:
---

# ConfigMap for lvmd Configuration (Superseded)

**This decision was reverted in [PR #566](https://github.com/openshift/lvm-operator/pull/566).** The ConfigMap approach was replaced back to file-based configuration because a single ConfigMap cannot represent per-node configuration differences. The current implementation uses `FileConfig` reading/writing `/etc/topolvm/lvmd.yaml` on the host filesystem (see `internal/controllers/vgmanager/lvmd/lvmd.go`).

## Context and Problem Statement

lvmd needs per-node configuration specifying which volume groups and device classes to manage. Originally this was a host file. PR #480 migrated to a ConfigMap for observability, but this was reverted because a single ConfigMap can't hold different configs for different nodes.

## Considered Options

1. Host filesystem file (`/etc/topolvm/lvmd.yaml`) written by VG Manager
2. Kubernetes ConfigMap in the operator namespace

## Decision Outcome

Option 2 was chosen in PR #480 but **reverted in PR #566** back to option 1 because per-node config differences cannot be expressed in a single ConfigMap.

The convention from this experience: when querying VG state, always query the host directly rather than relying on config files, because file writes can fail silently and leave stale config (#146).

## More Information

* [PR #480](https://github.com/openshift/lvm-operator/pull/480) — ConfigMap migration (superseded)
* [PR #566](https://github.com/openshift/lvm-operator/pull/566) — revert to file-based config
* [PR #14](https://github.com/openshift/lvm-operator/pull/14) — original multi-node config concern
