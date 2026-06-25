# Native LVM RAID Support

## Summary

LVM provides native software RAID (Redundant Array of Independent Disks) capabilities through the dm-raid (device-mapper RAID) target, supporting RAID levels 1, 4, 5, 6, and 10. RAID0 (striping without redundancy) is deliberately excluded as it provides no data protection. This allows data redundancy and protection directly within LVM without relying on external tools like mdadm.

**Advantages**:
- Unified management through LVM tooling — no separate RAID layer to configure and monitor.
- Per-device-class RAID configuration — different device classes can use different RAID levels.
- Integration with existing LVM features such as device management and volume group operations.

**Disadvantages**:
- While LVM technically supports RAID-backed thin pools, this implementation uses thick provisioning for RAID device classes to reduce complexity. Snapshots and clones are not available for RAID device classes.
- Reduced usable capacity due to RAID overhead (mirroring, parity).
- Write amplification — RAID1 doubles every write, RAID5/6 require read-modify-write cycles for parity updates. This is particularly relevant for write-heavy edge workloads (sensor data, logs).
- Recovery from device failures requires manual intervention. During RAID rebuild, I/O performance degrades as the array resynchronizes data to the replacement device.

LVMS will support native LVM RAID by introducing a `RAIDConfig` on the `DeviceClass` API. When configured, the device class uses thick provisioning and all logical volumes created within it are RAID protected at the specified level.

## Design Details

- A new `RAIDConfig` field is added to the `DeviceClass` API, mutually exclusive with `ThinPoolConfig`.
- When `RAIDConfig` is set, the device class uses thick provisioning. Although LVM supports RAID-backed thin pools, this implementation keeps RAID and thin provisioning mutually exclusive to reduce complexity. Snapshots and clones are not available for RAID device classes.
- The user specifies devices and RAID level. The operator is responsible for translating the configuration into the correct LVM and TopoLVM parameters.
- All RAID configuration fields are immutable after the device class is created.
- Dynamic device discovery is not available for RAID device classes. Any `deviceDiscoveryPolicy` value is ignored when `raidConfig` is set — the operator always behaves as `Static`.
- RAID health is monitored by the VG Manager and reported in `LVMVolumeGroupNodeStatus`. Recovery from degraded state is performed manually by the administrator.

### API

`LVMClusterSpec.Storage.DeviceClass.RAIDConfig` configures RAID for a device class. It is mutually exclusive with `ThinPoolConfig`.

#### RAIDType

A string enum representing the LVM RAID level.

- `raid1` — Mirroring. Requires at least 2 devices.
- `raid4` — Striping with dedicated parity device. Requires at least 3 devices.
- `raid5` — Striping with distributed parity. Requires at least 3 devices.
- `raid6` — Striping with double distributed parity. Requires at least 5 devices (3 data stripes + 2 parity devices).
- `raid10` — Striped mirrors. Requires at least 4 devices.

#### RAIDConfig

- **Type** (required): The LVM RAID level. One of `raid1`, `raid4`, `raid5`, `raid6`, `raid10`.
- **Mirrors** (optional): Number of mirror copies. Only valid for `raid1` and `raid10`. Default is 1 (2 total copies: original + 1 mirror).
- **Stripes** (optional): Number of data stripes. Only valid for `raid4`, `raid5`, `raid6`, and `raid10`. When not specified, LVM uses its default (typically all available devices minus parity). When specified, the value is fixed and does not change when devices are added or removed.
- **StripeSize** (optional): Size of each stripe chunk. Only valid for `raid4`, `raid5`, `raid6`, and `raid10`. Default is 64Ki.

#### Example LVMCluster CR

```yaml
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: my-lvmcluster
spec:
  storage:
    deviceClasses:
    - name: raid-vg
      raidConfig:
        type: raid1
      deviceSelector:
        paths:
        - /dev/sda
        - /dev/sdb
```

```yaml
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: my-lvmcluster
spec:
  storage:
    deviceClasses:
    - name: raid5-vg
      raidConfig:
        type: raid5
        stripeSize: 256Ki
      deviceSelector:
        paths:
        - /dev/sda
        - /dev/sdb
        - /dev/sdc
        - /dev/sdd
```

```yaml
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: my-lvmcluster
spec:
  storage:
    deviceClasses:
    - name: raid1-with-optional
      raidConfig:
        type: raid1
      deviceSelector:
        paths:
        - /dev/sda
        - /dev/sdb
        optionalPaths:
        - /dev/sdc
```

In this example, `/dev/sda` and `/dev/sdb` are required and sufficient for raid1 (minimum 2 devices). If `/dev/sdc` is present on a node, it is added to the volume group, providing additional raw capacity for new RAID LVs. If absent, the device class still functions with the two required devices.

### Validation

#### Creation

| Rule | Error |
|------|-------|
| `raidConfig` and `thinPoolConfig` both set | raidConfig and thinPoolConfig are mutually exclusive |
| `mirrors` set on raid4, raid5, or raid6 | mirrors is only valid for raid1 and raid10 |
| `stripes` set on raid1 | stripes is only valid for raid4, raid5, raid6, and raid10 |
| `stripeSize` set on raid1 | stripeSize is only valid for raid4, raid5, raid6, and raid10 |
| Neither `deviceSelector.paths` nor `deviceSelector.optionalPaths` set | at least one of paths or optionalPaths is required when raidConfig is set |
| Device count in `paths` below RAID minimum (when only `paths` is used) | raid6 requires at least 5 devices, got 4 |
| raid10 device count not divisible by (mirrors + 1) | raid10 with mirrors=1 requires an even number of devices; with mirrors=2, the count must be a multiple of 3 |
| `stripeSize` not a power of 2 | stripeSize must be a power of 2 (e.g., 64Ki, 128Ki, 256Ki, 512Ki) |

#### Updates

All fields within `raidConfig` are immutable once the device class is created. Changing `type`, `mirrors`, `stripes`, or `stripeSize` is rejected by the webhook. Removing `raidConfig` from an existing device class is also rejected.

Adding new device paths to `deviceSelector.paths` or `deviceSelector.optionalPaths` is allowed. Removing device paths from either list is allowed — at least one of `paths` or `optionalPaths` must remain non-empty. It is the administrator's responsibility to remove the device from the volume group on the node before updating the CR — the operator does not perform device removal from the VG.

#### Interactions with Existing Fields

| Field | Behavior with RAIDConfig |
|-------|--------------------------|
| `thinPoolConfig` | Mutually exclusive |
| `filesystemType` | Works as normal (xfs or ext4 on the RAID LV) |
| `default` | Works as normal |
| `nodeSelector` | Works as normal |
| `deviceDiscoveryPolicy` | Ignored — always behaves as Static |
| `forceWipeDevicesAndDestroyAllData` | Works as normal — devices are wiped individually before VG creation, prior to any RAID LV being created |

#### Volume Expansion

PVC expansion (via `lvextend`) is supported for RAID logical volumes. LVM extends all RAID legs — mirrors, data stripes, and parity — transparently. The raw space consumed by the extension is multiplied by the same RAID overhead factor used for capacity reporting (e.g., extending a raid1 LV by 10 GiB consumes 20 GiB of raw VG space).

After extension, LVM performs a partial resync of the newly added region to establish mirror or parity consistency. This resync is visible in `raidStatus.lvHealth[].syncPercent` and may trigger the `RAIDSyncSlow` alert if it takes longer than 30 minutes. The resync competes with normal I/O.

Free space must be available on each PV that hosts a RAID leg, not just as total VG free space. This is generally satisfied when all devices in the VG are of similar size, but can fail if devices have significantly different capacities and some PVs are full while others have space.

#### VolumeSnapshot and Clone Support

RAID device classes use thick provisioning, which does not support CSI volume snapshots or clones in TopoLVM. The operator does not create a `VolumeSnapshotClass` for RAID device classes. If a user creates a `VolumeSnapshot` targeting a PVC backed by a RAID device class, the CSI driver rejects the request. This is consistent with the existing behavior for any thick-provisioned device class.

### Volume Group Manager

The VG Manager creates and extends volume groups in the same way as it does today. RAID is applied at the logical volume level, not the volume group level, so `vgcreate` and `vgextend` operations are unchanged.

#### Device Validation

RAID device count validation happens at two levels:

1. **Webhook (CR-level)**: When `deviceSelector.paths` is set, validates the number of required paths against the RAID minimum device count and raid10 divisibility constraints. When only `optionalPaths` is used, the webhook cannot validate device counts since all devices may be absent — validation is deferred entirely to the VG Manager. This catches configuration errors early when possible.
2. **VG Manager (node-level)**: The VG Manager performs final validation of the actual device set on each node. Required paths that are missing or unusable cause the device class status to be set to Failed. Optional paths that are absent are silently skipped. The VG Manager then validates that the total usable device count (required + available optional) meets the RAID minimum device count and geometry constraints (e.g., raid10 divisibility). If validation fails, the device class status is set to Failed on the node and the failure is propagated to the `LVMCluster` status. This is the authoritative check — the webhook cannot perform it because the final device count depends on which optional paths are present on each node.

This two-level approach ensures that the webhook catches structural errors (wrong device count, invalid RAID geometry for required paths) while the VG Manager performs the authoritative validation of the final device set on each node, including optional paths.

#### RAID Health Monitoring

The VG Manager monitors RAID health during each reconciliation cycle by querying LVM for RAID logical volume status. It checks sync progress and health status for all RAID LVs in the volume group.

RAID health is reported in the `LVMVolumeGroupNodeStatus` via a new `RAIDStatus` field on `VGStatus`:

- **Status**: Overall RAID health — `Healthy`, `Degraded`, or `Failed`.
- **LVHealth**: Per-logical-volume details including RAID type, sync progress percentage, and LVM health status (empty for healthy, or `partial`, `refresh needed`, `mismatches exist`).

When any RAID LV is degraded, the overall `VGStatus.Status` is set to `Degraded`.

Example status:

```yaml
status:
  nodeStatus:
  - node: worker-1
    deviceClasses:
    - name: raid-vg
      status: Degraded
      devices:
      - /dev/sda
      - /dev/sdb
      raidStatus:
        status: Degraded
        lvHealth:
        - name: lv-pvc-abc123
          raidType: raid1
          syncPercent: 100
        - name: lv-pvc-def456
          raidType: raid1
          syncPercent: 42
          healthStatus: "partial"
```

#### lvmd.yaml Configuration

The VG Manager generates the lvmd.yaml configuration for TopoLVM with the device class set to `thick` type. RAID parameters are passed through the existing `lvcreate-options` field, which TopoLVM already supports for passing arbitrary flags to `lvcreate`.

When `stripes` is specified in `raidConfig`, the operator passes `--stripes` to `lvcreate` via `lvcreate-options`. When not specified, `--stripes` is omitted and LVM uses its default. The stripe count is fixed at creation time and does not change when devices are added to the volume group.

The operator also sets a new `overhead-factor` field so that TopoLVM can correctly report usable capacity to the scheduler (see [TopoLVM Changes](#topolvm-changes)).

Example generated lvmd.yaml:

```yaml
device-classes:
- name: raid-vg
  volume-group: raid-vg
  type: thick
  overhead-factor: 2.0
  lvcreate-options:
  - "--type"
  - "raid1"
  - "-m"
  - "1"
```

The RAID overhead factor is computed based on the RAID level. When `stripes` is specified in `raidConfig`, the overhead factor uses the stripe count directly. When `stripes` is not specified, LVM defaults to using all available devices, so the overhead factor is derived from the device count on the node.

| RAID level | Overhead factor (stripes specified) | Overhead factor (stripes not specified) |
|-----------|-------------------------------------|----------------------------------------|
| raid1 | mirrors + 1 (e.g., 2.0 for 1 mirror) | mirrors + 1 |
| raid4, raid5 | (stripes + 1) / stripes | devices / (devices - 1) |
| raid6 | (stripes + 2) / stripes | devices / (devices - 2) |
| raid10 | mirrors + 1 | mirrors + 1 |

For raid4, raid5, and raid6 without user-specified stripes, the overhead factor depends on the device count, which may vary per node when `optionalPaths` are used. Since the VG Manager generates lvmd.yaml per node, it computes the overhead factor using the actual number of devices present on that node (required + available optional).

Note: LVM RAID also creates per-device metadata sub-LVs (`_rmeta_#`) that consume approximately one physical extent (typically 4 MiB) per device. This overhead is not included in the overhead factor calculation. For practical volume sizes it is negligible, but provisioning many small volumes on a nearly-full VG may encounter slightly less usable space than the overhead factor predicts.

### Day 2 Operations

#### Adding Devices

Adding new device paths to `deviceSelector.paths` or `deviceSelector.optionalPaths` is supported. The operator extends the volume group with the new device. Existing RAID logical volumes are not modified — the additional space is available for new logical volumes.

Adding devices is subject to RAID geometry validation. The VG Manager validates the total device count on each node after accounting for the new devices. If the resulting count violates RAID constraints (e.g., raid10 with mirrors=1 requires an even number of devices), the device class status is set to Failed on that node and the failure is propagated to the `LVMCluster` status. The webhook does not validate device additions against geometry constraints beyond the minimum device count, since the final count depends on which optional paths are present on each node.

#### Removing Devices

The operator does not perform device removal or replacement from RAID arrays. Removing a path from `deviceSelector.paths` or `deviceSelector.optionalPaths` does not cause the operator to remove the device from the volume group — it only updates the CR to reflect the current state. All physical changes — removing a device from the volume group, replacing a failed device, or running `pvmove` / `vgreduce` — must be performed by the cluster administrator directly on the node using LVM commands. Once the node-level state is updated, the administrator updates the `LVMCluster` CR to reflect the actual device set.

Removal of paths from either `deviceSelector.paths` or `deviceSelector.optionalPaths` is allowed as long as at least one of the two lists remains non-empty. It is the administrator's responsibility to ensure that the devices have already been removed from the volume group on the node before updating the CR — the operator does not verify consistency between the CR and the on-disk VG membership.

#### Degraded State Behavior

When a RAID array is degraded (one or more devices failed but the array is still functional):

- **Existing PVCs** remain usable. Applications can continue reading and writing to their volumes.
- **New PVCs** can still be provisioned on the degraded volume group. LVM allows creating new RAID LVs on a VG with a degraded array, as long as sufficient healthy devices remain. However, new LVs created while degraded offer reduced redundancy until the array is repaired.
- **VGStatus** is set to `Degraded`, and the `raidStatus` field reports per-LV health details.

A degraded array triggers a critical `RAIDDegraded` alert — a second device failure on a degraded raid1 or raid5 array results in data loss.

#### Device Failure and Recovery

Recovery from a failed device is a manual process. The operator does not perform RAID repair or device replacement — the administrator must perform all changes on the node first, then update the `LVMCluster` CR to reflect the actual state:

1. The VG Manager detects degraded RAID logical volumes and reports the degraded status in `LVMVolumeGroupNodeStatus`.
2. The administrator replaces the failed physical device.
3. The administrator performs RAID repair using LVM commands on the node (e.g., `lvconvert --repair`, `pvmove`, `vgreduce`, `vgextend`).
4. The administrator updates the `LVMCluster` CR to reflect the new device path if it has changed.
5. The VG Manager detects the restored health and updates the status back to `Healthy`.

During rebuild (steps 3–5), I/O performance degrades as the array resynchronizes data. The rebuild progress is visible in the `raidStatus.lvHealth[].syncPercent` field. LVM does not expose rebuild throttling controls — the resync competes with normal I/O.

For detailed step-by-step recovery procedures with commands covering device replacement (same path and different path) and VG reduction without replacement, see the [RAID Recovery section in the Troubleshooting Guide](../troubleshooting.md#recovery-from-raid-device-failure).

#### Initial Sync

When a RAID logical volume is created, LVM performs an initial sync to establish parity or mirror consistency. This can be slow for large devices and affects initial provisioning latency. LVM supports a `--nosync` flag to skip this for raid1, raid4, raid5, and raid10 (not supported for raid6). This implementation does not expose `--nosync` — initial sync is always performed to ensure data integrity from the start. A future enhancement could expose this as an option for environments where devices are known-clean.

#### Changing RAID Options

Not supported in this implementation. While LVM itself supports changing mirror count (`lvconvert --mirrors`) and stripe size (`lvconvert --stripesize`) on existing RAID LVs, the operator treats all `raidConfig` fields as immutable after creation to reduce operational risk. To change RAID configuration, the administrator must create a new device class, migrate workloads, and delete the old device class. This restriction may be relaxed in future iterations.

### TopoLVM Changes

Changes to the TopoLVM fork are limited to capacity reporting. LV creation uses the existing `lvcreate-options` mechanism and requires no TopoLVM modifications.

#### Capacity Reporting

TopoLVM reports available storage capacity to the Kubernetes scheduler for topology-aware pod placement. Without RAID awareness, TopoLVM reports raw VG free space, which overstates the usable capacity. For example, a VG with 200GB free would report 200GB available, but a 120GB RAID1 logical volume requires 240GB of raw space and would fail to provision.

To fix this, TopoLVM reads a new `overhead-factor` field from the lvmd.yaml device class configuration. The reported available capacity becomes:

```
available = vg_free / overhead_factor
```

The `overhead-factor` defaults to 1.0 (no overhead) when not specified, maintaining backward compatibility for non-RAID device classes.

### Monitoring and Alerts

#### Metrics

The VG Manager exposes the following Prometheus metrics for RAID device classes. Metrics are per device class, not per logical volume — a device class with many PVCs should produce one alert, not hundreds.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lvms_raid_health_status` | Gauge | `node`, `device_class` | 0 = healthy, 1 = degraded, 2 = failed. Reflects the worst health across all RAID LVs in the device class |
| `lvms_raid_sync_in_progress` | Gauge | `node`, `device_class` | 1 if any RAID LV in the device class is resynchronizing, 0 otherwise |

Per-logical-volume health details (RAID type, sync progress, health status) are available in the `LVMVolumeGroupNodeStatus` CR via the `raidStatus` field for programmatic access and troubleshooting.

#### Alerts

RAID alerts are included in the existing `PrometheusRule` (`prometheus-lvmo-rules`) and fire automatically when metrics indicate a problem. Each alert fires once per device class per node — not per logical volume.

| Alert | Severity | Condition | Description |
|-------|----------|-----------|-------------|
| `RAIDDegraded` | critical | `lvms_raid_health_status == 1` for 5m | A RAID device class has one or more degraded logical volumes. The array is still functional but has reduced redundancy. Administrator should inspect `LVMVolumeGroupNodeStatus` for per-LV details, replace the failed device, and run `lvconvert --repair`. |
| `RAIDFailed` | critical | `lvms_raid_health_status == 2` for 1m | A RAID device class has one or more failed logical volumes. Data may be unavailable or lost. Immediate intervention required. |
| `RAIDSyncSlow` | warning | `lvms_raid_sync_in_progress == 1` for 30m | A RAID device class has been resynchronizing for more than 30 minutes. This may indicate a slow or stalled rebuild. I/O performance is degraded while sync is in progress. Administrator should check `raidStatus.lvHealth` in `LVMVolumeGroupNodeStatus` for per-LV sync progress. |

Alerts follow the existing LVMS pattern: `description` and `message` annotations with `$labels.device_class` and `$labels.node` for identifying the affected device class and node.

### Prerequisites

The `dm-raid` kernel module must be available on all nodes where RAID device classes are configured. RHEL and CoreOS (used by OpenShift) include this module by default. The VG Manager should verify module availability and report a clear error if it is missing.

### RAID LV Naming

RAID logical volumes follow the same naming convention as non-RAID LVs (based on the PVC UID). No special naming or labeling is applied to distinguish RAID LVs from non-RAID LVs — the RAID type is a property of the device class, and all LVs within a RAID device class are RAID-protected.

### Testing Strategy

- **Unit tests**: Overhead factor calculation for each RAID level, validation logic (minimum device counts, raid10 divisibility, stripes/stripeSize constraints, mutual exclusivity with ThinPoolConfig, immutability), and lvmd.yaml generation with RAID parameters (including optional stripes pass-through).
- **Unit tests (VG Manager)**: RAID health parsing from LVM output, status reporting logic, device validation at the node level.
- **E2E tests**: Creating an LVMCluster with RAID device classes, provisioning PVCs on RAID-backed storage classes, verifying correct `lvcreate` flags via LV inspection, and validating that VolumeSnapshot requests are rejected for RAID device classes. E2E tests require nodes with multiple available block devices.
- **Webhook tests**: Validation of all creation and update rules in the validation table, including rejection of invalid configurations and immutability enforcement.
