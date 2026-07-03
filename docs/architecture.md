# LVMS Architecture

This document captures the high-level architecture and design rationale for LVMS (Logical Volume Manager Storage).

## Component Overview

![component diagram](https://www.plantuml.com/plantuml/png/XP8nJyCm48Lt_ugdii3G0GH2oe3Q9eY5WjIACg0ELdAq5OvjsMSaXFZlk4c46apAUkzzB_SkddYMZaEjX9NbczmGHlUh-HAvgQtHfDcFy2c0bpZ5eoKdsRXrDyXLy4mEf_cYE5lZP5eKvxEBlRWoAjI4EsU2nLpg6Fn3jLehzSdKy60gMhBau7zRlqHlusJXtWe-ShFBwVNjLVC9izcLKg5r76WnizyJu_7DG1baACWgy-7_PDBhP7YMN6vfq9_U9JAv8yda8SJ06f5E6Xs2KbUe6xE7wcplhUr8P7A-h9Dz1sFJ24qyRtSQrXWrd9XMJCzod3t-AZ8ysMhVLuX_lOF_Pq6lYahsiH31DuoOaAv2hRu1)

| Component            | Kind       | Runners                                                                                             |
|----------------------|------------|-----------------------------------------------------------------------------------------------------|
| LVM Operator         | Deployment | [manager](design/lvm-operator-manager.md)                                                           |
|                      |            | [topolvm-controller](https://github.com/topolvm/topolvm/blob/main/docs/topolvm-controller.md)       |
|                      |            | [csi-provisioner](https://github.com/kubernetes-csi/external-provisioner/blob/master/doc/design.md) |
|                      |            | [csi-resizer](https://github.com/kubernetes-csi/external-resizer#csi-resizer)                       |
|                      |            | [liveness-probe](https://github.com/kubernetes-csi/livenessprobe#liveness-probe)                    |
|                      |            | [csi-snapshotter](https://github.com/kubernetes-csi/external-snapshotter#csi-snapshotter)           |
| Volume Group Manager | DaemonSet  | [vg-manager](design/vg-manager.md)                                                                  |
|                      |            | [topolvm-node](https://github.com/topolvm/topolvm/blob/main/docs/topolvm-node.md)                   |
|                      |            | [csi-registrar](https://github.com/kubernetes-csi/node-driver-registrar#node-driver-registrar)      |
|                      |            | [liveness-probe](https://github.com/kubernetes-csi/livenessprobe#liveness-probe)                    |

See also the [upstream TopoLVM design](https://github.com/topolvm/topolvm/blob/main/docs/design.md) for further details on the CSI driver internals.

## Why LVMS Embeds TopoLVM

LVMS depends on [TopoLVM](https://github.com/topolvm/topolvm), an upstream CSI driver for logical volume management on Kubernetes. Unlike typical CSI driver deployments where the driver runs as a separate workload, LVMS compiles TopoLVM controllers directly into its own binaries (`lvm-operator` and `vg-manager`).

This is an intentional design choice for edge and single-node clusters:
- Fewer pods reduce resource overhead on constrained nodes.
- Startup time improves because there is no dependency chain between separate deployments.
- A single binary simplifies upgrades and rollback.

The TopoLVM dependency is vendored via `go.mod` with a `replace` directive pointing to the downstream fork at `github.com/openshift/topolvm`. See [upstream.md](upstream.md) for the upstream contribution workflow and [dependency-management.md](dependency-management.md) for update procedures.

## Reconciliation Lifecycle

When a user creates an `LVMCluster` CR, the operator drives the system through the following stages:

1. **LVMCluster controller** validates the CR and creates subordinate resources: `LVMVolumeGroup` CRs (one per device class), TopoLVM `CSIDriver`, `StorageClass`, `VolumeSnapshotClass`, and the VG Manager `DaemonSet`.
2. **VG Manager** (running as a privileged DaemonSet on each matching node) watches for `LVMVolumeGroup` CRs, discovers block devices matching the device selector, creates LVM physical volumes and volume groups, and optionally provisions thin pools.
3. **Status reporting**: each VG Manager instance creates or updates an `LVMVolumeGroupNodeStatus` CR reflecting the volume group state on its node. The LVMCluster controller aggregates these to set the cluster-level status.

VG Manager periodic reconciliation depends on the configuration: RAID device classes requeue every 30 seconds for health monitoring, Dynamic discovery policy requeues every 30 seconds for device discovery, and explicit device paths with Static policy do not periodically requeue. The LVMCluster controller requeues every 1 minute unconditionally.

## Data Flow: PVC to Logical Volume

When a workload requests storage via a `PersistentVolumeClaim` using an LVMS `StorageClass`:

1. The `StorageClass` is configured with `volumeBindingMode: WaitForFirstConsumer`, so the PVC stays `Pending` until a pod is scheduled.
2. The Kubernetes scheduler uses TopoLVM's topology-aware scheduling to select a node with sufficient free capacity in the target volume group.
3. The TopoLVM controller (embedded in the operator deployment) creates a `LogicalVolume` CR.
4. The TopoLVM node plugin (embedded in VG Manager on the target node) provisions the LVM logical volume and exposes it via CSI.
5. The kubelet mounts the volume into the pod.

## CRD Relationships

```
LVMCluster (user-facing, singleton per namespace)
 └── LVMVolumeGroup (internal, one per device class)
      └── LVMVolumeGroupNodeStatus (internal, one per node, contains per-VG status entries)
```

- **LVMCluster**: the only CR users create directly. Defines device classes, device selectors, thin pool configuration, and node selectors. Only one LVMCluster is supported per cluster.
- **LVMVolumeGroup**: created and managed by the LVMCluster controller. Represents a single volume group definition. Users do not create these directly.
- **LVMVolumeGroupNodeStatus**: created by VG Manager on each node (named after the node). Contains a list of `VGStatus` entries — one per volume group on that node — reporting device list, excluded devices, and (if configured) RAID health status. Note: the status data is in `.Spec.LVMVGStatus`, not `.Status`.

## API Version: v1alpha1

Despite the `v1alpha1` designation, this is the production API. The name is a historical artifact — upgrading to `v1alpha2` or `v1beta1` would require a full CRD migration path, webhook updates, and a coordinated rollout across upstream and downstream consumers, and that cost has not been justified given the API's effective stability. Treat it as stable: new fields should be optional and backward-compatible, and breaking changes require migration support.

## Cleanup and Finalizer Hierarchy

LVMS uses a three-level finalizer hierarchy to ensure safe deletion:

1. **`lvmcluster.topolvm.io`** on `LVMCluster`: blocks deletion until all PVCs using LVMS StorageClasses are removed and all Retain-policy PVs are cleaned up.
2. **`cleanup.vgmanager.node.topolvm.io/{nodeName}`** on `LVMVolumeGroup`: per-node finalizer added by VG Manager. Removed only after the node-local cleanup is complete (logical volumes deleted, volume group removed, physical volumes cleared, lvmd config cleaned).
3. **`delete-protection.lvm.openshift.io`** on `LVMVolumeGroupNodeStatus`: prevents premature deletion of node status during cleanup.

This hierarchy ensures that storage is never orphaned: the cluster-level resource cannot be deleted until every node has completed its local cleanup.

## Webhook Validation

The `LVMCluster` CR is validated by an admission webhook at `/validate-lvm-topolvm-io-v1alpha1-lvmcluster`. Key validations include:

- Singleton enforcement (one LVMCluster per namespace)
- Device class uniqueness and at most one default class
- Device path validation (must be absolute, starting with `/dev/`)
- ThinPoolConfig and RAIDConfig immutability after creation
- Device path overlap detection across device classes
- Filesystem type validation (ext4 or xfs only)

Due to OLM constraints, the webhook is scoped to the operator namespace (`openshift-lvm-storage`). LVMCluster CRs created in other namespaces are not validated and are silently ignored by the operator.

## Key Design Decisions

- **DaemonSet for node operations**: VG Manager runs as a privileged DaemonSet rather than using a Job-based model. This allows continuous device monitoring and idempotent reconciliation without scheduling overhead.
- **Thin provisioning by default**: all volume groups use thin pools to enable snapshot and clone support. This introduces the thin/RAID flag conflict that prevents native LVM RAID (see README Known Limitations).
- **Embedded lvmd**: the lvmd gRPC daemon from TopoLVM runs in-process within VG Manager, configured via a file-based config at `/etc/topolvm/lvmd.yaml` on the host filesystem (a ConfigMap approach was tried and reverted — see [ADR-0009](decisions/0009-configmap-for-lvmd-config.md)).
- **Node removal controller**: a dedicated controller watches for node deletions and cleans up orphaned `LVMVolumeGroupNodeStatus` resources, preventing stale status from accumulating.

## Further Reading

- [LVM Operator Manager details](design/lvm-operator-manager.md)
- [VG Manager design](design/vg-manager.md)
- [Thin provisioning design](design/thin-provisioning.md)
- [RAID support design](design/raid-support.md)
- [Upstream TopoLVM workflow](upstream.md)
- [Dependency management](dependency-management.md)
- [Troubleshooting guide](troubleshooting.md)
