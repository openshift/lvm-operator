# LVMS Core Beliefs

Key design principles that guide LVMS development. For architecture and component design, see [architecture.md](architecture.md). For implementation conventions, see [conventions/](conventions/). For agent-specific guidance, see [AGENTS.md](../AGENTS.md).

## Safety-First LVM Operations

LVMS manages physical storage. A bug can destroy user data. The codebase errs on the side of caution at every layer.

- **Device wiping requires triple opt-in**: DeviceSelector configured, `ForceWipeDevicesAndDestroyAllData` set to `true`, and a per-node annotation preventing repeat wipes.
- **Device filtering is conservative**: better to miss a valid device than wipe user data. Time-based filtering (`deviceMinAge`) was removed because it gives false confidence — LVM's own system lock is the real guard.
- **Deletion blocks on active storage**: the LVMCluster finalizer blocks until PVCs are removed and Retain-policy PVs are cleaned up, emitting `ManualCleanupRequired` if user logical volumes exist.
- **Never force-remove corrupt LVs**: set state to Degraded with explanation. Failed is for new creations; Degraded is for existing VGs with active data.
- **All LVM commands accessing lvmdevices must run AsHost** — not container-level privileges.
- **Always pass `-Z y` to lvcreate for thin pools** — do not rely on host `lvm.conf` defaults.

## Defense-in-Depth Validation

Critical constraints are validated at CRD schema (kubebuilder markers), admission webhook, AND controller runtime. The webhook can be deleted.

LVMVolumeGroup has no webhook. Manual creation bypasses LVMCluster validation entirely. Invariants should be enforced in vg-manager, not just the webhook.

CEL XValidation for simple invariants (immutability). Webhook for cross-field logic.

## Features Ship Complete

New features should include observability from day one where possible. If a feature can degrade, it should surface that at launch. Alerts are typically scoped to one per deviceClass, not per PVC, to avoid flooding. (Established during RAID design review, PR #2337.)

## Greedy Mode Is Permanent

No device paths specified = LVMS takes all devices. Switching to explicit paths later requires cluster recreation. Nil DeviceSelector (dynamic discovery) is single-deviceClass only; multi-deviceClass requires explicit DeviceSelector.

## RAID Constraints

Immutable after creation. Mutually exclusive with thin provisioning. Requires explicit device paths — dynamic discovery is fundamentally incompatible. Day-2 device replacement uses `optionalPaths`. Manual VG recovery with `vgreduce --removemissing` is documented in troubleshooting.md as a manual workaround, not an automated LVMS operation.

## New Optional Fields: Nil Means Upgraded

When adding optional fields, `nil` means "CR was created before this field existed." Runtime should default nil to behavior preserving backward compatibility to avoid breaking existing clusters on upgrade.

## Separate Service Accounts per Workload

Each workload (operator Deployment, vg-manager DaemonSet) gets its own service account with minimal RBAC. Due to the single-binary architecture, topolvm-controller shares the operator SA and topolvm-node shares the vg-manager SA. VG Manager requires OpenShift `privileged` SCC.
