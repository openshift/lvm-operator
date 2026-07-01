# LVMS Concept Relationships

How LVMS concepts relate to each other. For term definitions, see [glossary.md](glossary.md). For architecture, see [architecture.md](../architecture.md). For design principles, see [core-beliefs.md](../core-beliefs.md).

## Resource Flow

The path from user intent to storage on disk:

```
LVMCluster (user creates)
 └── DeviceClass (1:N, defined in spec)
      ├── LVMVolumeGroup CR (1:1, operator creates)
      │    └── LVMVolumeGroupNodeStatus CR (1 per node, VG Manager creates)
      │         └── VGStatus (1 per VG on that node)
      ├── StorageClass (1:1, operator creates via SSA, named lvms-{name})
      ├── VolumeSnapshotClass (1:1, only if ThinPoolConfig set, named lvms-{name})
      └── On each matching node (VG Manager):
           ├── LVM VolumeGroup (tagged @lvms)
           │    └── ThinPool LV (if ThinPoolConfig set)
           └── LogicalVolume CRs → LVM LVs → PVs → PVCs → Pods
```

Key: LVMCluster is the only user-facing CR. Everything below it is operator-managed.

## Device Filter Chain

How a raw block device becomes a VG member. VG Manager runs this on every 30s reconcile cycle. A device must pass ALL filters to be "Available":

```
lsblk --json discovers all block devices
         │
         ▼
┌─ partOfDeviceSelector ─── Does it match Paths/OptionalPaths? (skip if no DeviceSelector)
│
├─ notReadOnly ──────────── Is ReadOnly=false?
│
├─ notSuspended ─────────── Is State != "suspended"?
│
├─ noInvalidPartitionLabel ─ Is PartLabel not bios/boot/reserved? (case-insensitive)
│
├─ onlyValidFilesystemSignatures
│    ├─ No FSType → pass
│    ├─ LVM2_member → pass if: same VG name, OR orphaned PV with free capacity
│    └─ Any other FSType → reject
│
├─ noChildren ───────────── Has no child block devices?
│
└─ usableDeviceType
     ├─ ROM → reject
     ├─ LVM partition → reject
     └─ Loop → pass only if not used by Kubernetes
```

Devices that fail filters are reported in `VGStatus.Excluded` with the filter name and reason. Two special errors (`ErrDeviceAlreadySetupCorrectly`, `ErrLVMPartition`) are suppressed from excluded reporting since they are expected post-setup.

## Reconciliation Flow

Two reconciliation loops run independently:

### LVMCluster Controller (Deployment, requeues every 1 min)

```
1. Validate LVMCluster CR
2. Create/update subordinate resources concurrently (goroutines + errors.Join):
   - CSIDriver
   - VG Manager DaemonSet
   - LVMVolumeGroup CRs (one per DeviceClass)
   - StorageClasses (via SSA)
   - VolumeSnapshotClasses (if thin pool)
   - SCCs (OpenShift only)
   - ServiceMonitor
3. Aggregate status from all LVMVolumeGroupNodeStatus CRs
4. Set LVMCluster conditions: ResourcesAvailable, VolumeGroupsReady
5. Compute state: Failed > Degraded > Ready (only Ready when ALL expected VGs on ALL valid nodes are Ready)
```

### VG Manager (DaemonSet, one per node, requeues every 30s)

```
1. List LVMVolumeGroup CRs matching this node
2. For each LVMVolumeGroup:
   a. Check for deletion (DeletionTimestamp set) → run cleanup if yes
   b. Check ForceWipeDevicesAndDestroyAllData → wipe if needed, return early
   c. List block devices via lsblk --json
   d. List existing VGs (filtered by @lvms tag)
   e. Run filter chain on all discovered devices
   f. Create VG (vgcreate) or extend VG (vgextend) with new devices
   g. Handle thin pool: create, extend, or validate chunk size / metadata
   h. Handle RAID: validate device count, check health, update metrics
   i. Handle device removal: detect removed paths, call vgreduce/pvremove
   j. Validate existing logical volumes (thin pool health, metadata %)
   k. Update LVMVolumeGroupNodeStatus with current state
3. Determine requeue:
   - Explicit device paths + Static policy → no periodic requeue
   - Dynamic policy → requeue in 30s
   - After VG creation/extension → always requeue (verification step)
```

## CRD Relationships

```
LVMCluster (namespace-scoped, singleton)
 │
 │ creates (ownerRef)
 ▼
LVMVolumeGroup (namespace-scoped, one per DeviceClass)
 │
 │ per-node finalizer: cleanup.vgmanager.node.topolvm.io/{nodeName}
 │ non-controller ownerRef on NodeStatus
 │
 ▼
LVMVolumeGroupNodeStatus (namespace-scoped, one per node)
 │
 │ controller ownerRef from LVMCluster (deletion propagation)
 │ non-controller ownerRef from each LVMVolumeGroup
 │ finalizer: delete-protection.lvm.openshift.io
 │
 │ contains
 ▼
VGStatus[] (one entry per VolumeGroup on this node)
```

Cluster-scoped resources (CSIDriver, StorageClass, VolumeSnapshotClass, SCC) use labels for ownership since OwnerReferences don't work cross-namespace. Label: `app.kubernetes.io/managed-by: lvms-operator`.

## StorageClass Lifecycle

How a DeviceClass becomes a Kubernetes StorageClass:

1. LVMCluster controller reads `DeviceClass.StorageClassOptions`
2. Builds StorageClass object with name `lvms-{deviceClassName}`
3. Copies `AdditionalParameters` from user, then overwrites with LVMS-owned keys:
   - `topolvm.io/device-class` = device class name
   - `csi.storage.k8s.io/fstype` = filesystem type
4. Copies `AdditionalLabels` from user, then sets managed labels
5. Applies via Server-Side Apply with field owner `lvms-operator` and `ForceOwnership`
6. Sets default SC annotation (`storageclass.kubernetes.io/is-default-class`) if DeviceClass is default and no other default exists

The SSA field manager model ensures LVMS-owned keys cannot be overridden by user kubectl edits. Day-2 changes to AdditionalLabels reconcile automatically.

## Deletion Flow

When an LVMCluster is deleted:

```
1. LVMCluster finalizer (lvmcluster.topolvm.io) blocks deletion
2. Gate 1: Check for active PVCs using LVMS StorageClasses
   └── If found → block, requeue
3. Gate 2: Check for Retain-policy PVs
   └── If found → block, requeue
4. processDelete: delete subordinate resources
5. For each LVMVolumeGroup:
   a. Each VG Manager removes its per-node finalizer after cleanup:
      - Delete thin pool LV
      - Remove VG (vgremove)
      - Remove PVs (pvremove)
      - Clean lvmd config
   b. Once all per-node finalizers are removed, LVMVolumeGroup is deleted
6. Stale node finalizer cleaner handles nodes that disappeared
7. LVMCluster finalizer removed → LVMCluster deleted
```

If ReclaimPolicy=Retain and user logical volumes exist, VG Manager emits `ManualCleanupRequired` event and blocks cleanup.

## Capacity and Scheduling

How LVMS-aware scheduling works:

1. VG Manager (via embedded TopoLVM node) sets capacity annotation on each Node: `capacity.topolvm.io/{deviceClassName}` = available bytes
2. PVC created with LVMS StorageClass (WaitForFirstConsumer binding mode)
3. Kubernetes scheduler uses TopoLVM's topology-aware scheduling to pick a node with sufficient capacity
4. TopoLVM controller creates LogicalVolume CR targeting that node
5. VG Manager provisions the LVM LV via TopoLVM node plugin
6. PVC controller checks pending PVCs — if no node has enough capacity, emits `NotEnoughCapacity` warning event on the PVC
