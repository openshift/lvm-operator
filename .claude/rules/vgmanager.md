---
paths:
  - "internal/controllers/vgmanager/**"
description: VG Manager safety rules, LVM command handling, and device filtering
---

## LVM Command Safety

- All LVM commands accessing lvmdevices must run `AsHost` — not container-level privileges
- Always pass `-Z y` to `lvcreate` for thin pools — never rely on host `lvm.conf` defaults
- Always handle errors from LVM host commands — never silently swallow
- Commands writing to both stdout and stderr must use `CombinedOutput` (`wipefs`/`dmsetup` race)
- After wiping devices, return early — let the next reconcile re-list (immediate re-listing returns stale data)

## Device Filtering

- Conservative filtering: better to miss a valid device than wipe user data
- Device wiping requires triple opt-in: DeviceSelector + `ForceWipeDevicesAndDestroyAllData` + per-node annotation
- VG ownership determined by `@lvms` LVM tag — force-wipe eligibility is a separate check
- Don't create PVs with `pvcreate` — pass raw devices to `vgcreate`/`vgextend`
- Filter out VGs not created by the operator when iterating existing VGs
- Compare symlink-resolved paths, not raw kname, for device filtering
- Use lsblk `TYPE` field ('loop') not Major number for loop devices

## Reconciliation

- VG Manager reconciles every 30 seconds (poll-based, not event-driven for device changes)
- VGManager must not report healthy until CSI driver registration is confirmed in kubelet
- Don't fail-fast on multi-node operations — continue for healthy nodes, aggregate errors

Full conventions: [docs/conventions/safety.md](../../docs/conventions/safety.md), [docs/conventions/reconciliation.md](../../docs/conventions/reconciliation.md)
