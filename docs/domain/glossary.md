# LVMS Domain Glossary

Canonical LVMS terminology. Each term maps to a Go type or CRD field. For architecture, see [architecture.md](../architecture.md). For concept relationships, see [concepts.md](concepts.md). For design principles, see [core-beliefs.md](../core-beliefs.md).

## LVMCluster

The primary user-facing CR (`api/v1alpha1/lvmcluster_types.go`). Defines device classes, tolerations, and the desired storage configuration. Singleton — only one per cluster. Creates all subordinate resources (see [concepts.md § Resource Flow](concepts.md#resource-flow)).

**Gotcha:** CRs created outside `openshift-lvm-storage` namespace bypass webhook validation and are silently ignored.

## DeviceClass

A named storage tier within an LVMCluster (`DeviceClass` struct). Maps 1:1 to an LVMVolumeGroup CR, a VolumeGroup on each matching node, a StorageClass (`lvms-{name}`), and optionally a VolumeSnapshotClass. Name must be a DNS-1123 label (lowercase, `[a-z0-9]([-a-z0-9]*[a-z0-9])?`).

**Gotcha:** The name flows into StorageClass, VolumeSnapshotClass, VG, and capacity annotation names — renaming is a breaking change. ThinPoolConfig and RAIDConfig are mutually exclusive.

## DeviceSelector

Rules for discovering block devices (`DeviceSelector` struct). `Paths` = mandatory (all must resolve on each node). `OptionalPaths` = optional (at least one must resolve). `ForceWipeDevicesAndDestroyAllData` = explicit wipe opt-in (see [core-beliefs.md § Safety-First](../core-beliefs.md#safety-first-lvm-operations)).

**Gotcha:** Nil DeviceSelector = "greedy mode" — permanent, cannot add explicit paths later (see [core-beliefs.md § Greedy Mode](../core-beliefs.md#greedy-mode-is-permanent)). Multi-DeviceClass always requires explicit DeviceSelector. Stable paths (`/dev/disk/by-id/` or `/dev/disk/by-path/`) recommended over kernel names (`/dev/sda`).

## DeviceDiscoveryPolicy

Controls whether VG Manager continuously discovers new devices or locks to install-time devices (`DeviceDiscoveryPolicySpec` at `lvmvolumegroupnodestatus_types.go`).

**Spec values:** `Static` (locked after VG creation) or `Dynamic` (continuous 30s discovery).

**Status values:** `Preconfigured` (explicit DeviceSelector paths override policy), `RuntimeStatic`, `RuntimeDynamic`.

**Gotcha:** Nil defaults depend on VG age: new VGs default to Static, existing VGs default to Dynamic for backward compatibility ("nil means upgraded"). RAID always forces Static. With explicit device paths, the spec policy is ignored — status always reports Preconfigured.

## VolumeGroup (LVM concept)

The LVM volume group on a node (`lvm.VolumeGroup` struct at `lvm/lvm.go`). Created via `vgcreate`, extended via `vgextend`. Name equals the DeviceClass name. All LVMS-managed VGs are tagged with `@lvms` (see below).

**Gotcha:** This is NOT a Kubernetes resource — do not confuse with LVMVolumeGroup (the CR). VG size is reported as a string in bytes with no suffix. A VG is valid even with zero physical volumes.

## ThinPoolConfig

Optional thin provisioning on a DeviceClass (`ThinPoolConfig` struct). `SizePercent` (default 90, range 10–100). `OverprovisionRatio` (required, range 1–100). `ChunkSizeCalculationPolicy` (Static or Host). `MetadataSize` (2Mi–16Gi). See [design/thin-provisioning.md](../design/thin-provisioning.md).

**Gotcha:** Mutually exclusive with RAIDConfig. Enables VolumeSnapshotClass creation. `Host` policy means the node's lvm2 decides chunk/metadata size — the spec value is ignored.

## RAIDConfig

Native LVM RAID on a DeviceClass (`RAIDConfig` struct). `Type` = raid1/raid4/raid5/raid6/raid10. `Mirrors` (raid1/raid10 only). `Stripes` (raid4/5/6/10). `StripeSize` (power of 2, default 64Ki). See [design/raid-support.md](../design/raid-support.md) and [core-beliefs.md § RAID Constraints](../core-beliefs.md#raid-constraints).

**Gotcha:** Entire struct is immutable after creation. Uses thick provisioning — no snapshot/clone support. Minimum device counts: raid1 = mirrors+1, raid4/5 = stripes+1, raid6 = stripes+2, raid10 = 2*(mirrors+1). Day-2 device replacement uses `optionalPaths`.

## StorageClassOptions

LVMS-managed StorageClass properties on a DeviceClass (`StorageClassOptions` struct). `ReclaimPolicy` (default Delete, immutable). `VolumeBindingMode` (default WaitForFirstConsumer, immutable). `AdditionalParameters` (immutable, max 16). `AdditionalLabels` (mutable, max 16). See [concepts.md § StorageClass Lifecycle](concepts.md#storageclass-lifecycle).

**Gotcha:** LVMS-owned keys (`topolvm.io/device-class`, `csi.storage.k8s.io/fstype`) are silently overwritten after user values are copied (merge order matters). `AdditionalLabels` is the only mutable field. ReclaimPolicy=Retain blocks LVMCluster deletion if PVs exist.

## FilesystemType

Filesystem for logical volumes (`DeviceFilesystemType` string). Values: `xfs` (default) or `ext4`. Set as `csi.storage.k8s.io/fstype` on the StorageClass. Immutable after creation.

## LVMVolumeGroup

Internal CR created 1:1 per DeviceClass (`api/v1alpha1/lvmvolumegroup_types.go`). Mirrors DeviceClass fields. Watched by VG Manager. See [architecture.md § CRD Relationships](../architecture.md#crd-relationships).

**Gotcha:** Users should never create these directly — has no admission webhook, so manual creation bypasses all LVMCluster validation. Status is NOT on this object; it's on LVMVolumeGroupNodeStatus. Per-node finalizer `cleanup.vgmanager.node.topolvm.io/{nodeName}` added by each VG Manager instance.

## LVMVolumeGroupNodeStatus

Per-node CR reporting actual VG state (`api/v1alpha1/lvmvolumegroupnodestatus_types.go`). Named after the node. Contains `Spec.LVMVGStatus` (JSON tag: `nodeStatus`) — a list of VGStatus entries, one per VG on that node.

**Gotcha:** The real status data is in `.Spec.LVMVGStatus`, not `.Status` (which is empty). This is an API design artifact. Dual ownership: LVMCluster as controller owner (deletion propagation) + LVMVolumeGroup as non-controller owner (multi-VG support).

## VGStatus

Per-VG status within LVMVolumeGroupNodeStatus (`VGStatus` struct). `Status` uses `VGStatusType`: Progressing, Ready, Failed, Degraded. Includes `Devices` (active PV paths), `Excluded` (filtered devices with reasons), `RAIDStatus`.

**Gotcha:** Failed automatically promotes to Degraded if the VG has existing devices — Failed is for new creations, Degraded is for existing VGs with active data.

## LogicalVolume

TopoLVM CR (vendored from `github.com/openshift/topolvm`). Created by TopoLVM controller when PVCs are provisioned. LVMS does not create these — only interacts during cleanup (removing stale finalizers for deleted nodes).

## ForceWipeDevicesAndDestroyAllData

Explicit wipe opt-in flag on DeviceSelector (`*bool`). See [core-beliefs.md § Safety-First](../core-beliefs.md#safety-first-lvm-operations) for the triple opt-in requirement.

## The @lvms Tag

LVM tag on every LVMS-managed VG (`lvm.DefaultTag = "@lvms"` at `lvm/lvm.go:53`). `ListVGs(ctx, true)` returns only tagged VGs.

**Gotcha:** If a VG exists with the right name but no tag, VG Manager won't find it. Manual fix: `vgchange {name} --addtag @lvms` on the node.
