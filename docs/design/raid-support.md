# Native LVM RAID Support

## Summary

LVM provides native RAID capabilities through the dm-raid (device-mapper RAID) target, supporting RAID levels 1, 4, 5, 6, and 10. This allows data redundancy and protection directly within LVM without relying on external tools like mdadm.

**Advantages**:
- Unified management through LVM tooling — no separate RAID layer to configure and monitor.
- Per-device-class RAID configuration — different device classes can use different RAID levels.
- Integration with existing LVM features such as device management and volume group operations.

**Disadvantages**:
- While LVM technically supports RAID-backed thin pools, this implementation uses thick provisioning for RAID device classes to reduce complexity. Snapshots and clones are not available for RAID device classes.
- Reduced usable capacity due to RAID overhead (mirroring, parity).
- Recovery from device failures requires manual intervention.

LVMS will support native LVM RAID by introducing a `RAIDConfig` on the `DeviceClass` API. When configured, the device class uses thick provisioning and all logical volumes created within it are RAID protected at the specified level.

## Design Details

- A new `RAIDConfig` field is added to the `DeviceClass` API, mutually exclusive with `ThinPoolConfig`.
- When `RAIDConfig` is set, the device class uses thick provisioning. Although LVM supports RAID-backed thin pools, this implementation keeps RAID and thin provisioning mutually exclusive to reduce complexity. Snapshots and clones are not available for RAID device classes.
- The user specifies devices and RAID level. The operator is responsible for translating the configuration into the correct LVM and TopoLVM parameters.
- All RAID configuration fields are immutable after the device class is created.
- Device discovery policy must be `Static` when RAID is configured — dynamic discovery is not appropriate for RAID arrays where device membership must be explicit.
- RAID health is monitored by the VG Manager and reported in `LVMVolumeGroupNodeStatus`. Recovery from degraded state is performed manually by the administrator.

### API

`LVMClusterSpec.Storage.DeviceClass.RAIDConfig` configures RAID for a device class. It is mutually exclusive with `ThinPoolConfig`.

#### RAIDType

A string enum representing the LVM RAID level. Note that `raid0` (striping without redundancy) is deliberately excluded as it provides no data protection.

- `raid1` — Mirroring. Requires at least 2 devices.
- `raid4` — Striping with dedicated parity device. Requires at least 3 devices.
- `raid5` — Striping with distributed parity. Requires at least 3 devices.
- `raid6` — Striping with double distributed parity. Requires at least 5 devices (LVM requires a minimum of 3 stripes for raid6, plus 2 parity devices).
- `raid10` — Striped mirrors. Requires at least 4 devices.

#### RAIDConfig

- **Type** (required): The LVM RAID level. One of `raid1`, `raid4`, `raid5`, `raid6`, `raid10`.
- **Mirrors** (optional): Number of mirror copies. Only valid for `raid1` and `raid10`. Default is 1 (2 total copies: original + 1 mirror).
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

### Validation

#### Creation

| Rule | Error |
|------|-------|
| `raidConfig` and `thinPoolConfig` both set | raidConfig and thinPoolConfig are mutually exclusive |
| `mirrors` set on raid4, raid5, or raid6 | mirrors is only valid for raid1 and raid10 |
| `stripeSize` set on raid1 | stripeSize is only valid for raid4, raid5, raid6, and raid10 |
| `deviceSelector.paths` not set | deviceSelector.paths is required when raidConfig is set |
| Device count below RAID minimum | raid6 requires at least 5 devices, got 4 |
| `deviceDiscoveryPolicy` set to Dynamic | dynamic device discovery is not supported with raidConfig |

#### Updates

All fields within `raidConfig` are immutable once the device class is created. Changing `type`, `mirrors`, or `stripeSize` is rejected by the webhook. Removing `raidConfig` from an existing device class is also rejected.

Adding new device paths to `deviceSelector.paths` is allowed. Removing device paths is blocked while RAID logical volumes have legs on the device.

#### Interactions with Existing Fields

| Field | Behavior with RAIDConfig |
|-------|--------------------------|
| `thinPoolConfig` | Mutually exclusive |
| `filesystemType` | Works as normal (xfs or ext4 on the RAID LV) |
| `default` | Works as normal |
| `nodeSelector` | Works as normal |
| `deviceDiscoveryPolicy` | Must be Static |

### Volume Group Manager

The VG Manager creates and extends volume groups in the same way as it does today. RAID is applied at the logical volume level, not the volume group level, so `vgcreate` and `vgextend` operations are unchanged.

#### Device Validation

Before creating or extending a volume group for a RAID device class, the VG Manager validates that the number of available devices meets the minimum required by the RAID level. If the count is insufficient, the device class status is set to Failed with an appropriate reason.

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

The RAID overhead factor is computed based on the RAID level and device count:

| RAID level | Overhead factor |
|-----------|----------------|
| raid1 | mirrors + 1 (e.g., 2.0 for 1 mirror) |
| raid4, raid5 | devices / (devices - 1) |
| raid6 | devices / (devices - 2) (minimum 5 devices) |
| raid10 | mirrors + 1 (e.g., 2.0 for 1 mirror) |

### Day 2 Operations

#### Adding Devices

Adding new device paths to `deviceSelector.paths` is supported. The operator extends the volume group with the new device. Existing RAID logical volumes are not modified — the additional space is available for new logical volumes.

#### Removing Devices

Not supported. The webhook rejects removal of device paths from `deviceSelector.paths` while RAID logical volumes exist on the device. To decommission a device, the administrator must first drain workloads, delete the associated PVCs, and ensure no RAID LV legs remain on the device.

#### Device Failure and Recovery

Recovery from a failed device is a manual process:

1. The VG Manager detects degraded RAID logical volumes and reports the degraded status in `LVMVolumeGroupNodeStatus`.
2. The administrator replaces the failed physical device.
3. The administrator performs RAID repair using LVM commands on the node (e.g., `lvconvert --repair`).
4. The administrator updates the `LVMCluster` CR to reflect the new device path if it has changed.
5. The VG Manager detects the restored health and updates the status back to `Healthy`.

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

RAID health is exposed through the existing `LVMVolumeGroupNodeStatus` CR. Prometheus metrics for RAID-specific conditions (degraded arrays, sync progress) can be added as a follow-up enhancement.

Administrators should monitor the `raidStatus` field in node status to detect degraded arrays promptly, as prolonged degradation increases the risk of data loss if additional devices fail.
