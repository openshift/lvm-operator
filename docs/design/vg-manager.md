# The Volume Group Manager

The Volume Group Manager manages a single controller/reconciler, which runs as `vg-manager` daemon set pods on a cluster. They are responsible for performing on-node operations for the node they are running on. They first identify disks that match the filters specified for the node. Next, they watch for the LVMVolumeGroup resource and create the necessary volume groups and thin pools on the node based on the specified deviceSelector and nodeSelector. Once the volume groups are created, vg-manager generates the `lvmd.yaml` configuration file for lvmd to use. Additionally, vg-manager updates the LVMVolumeGroupNodeStatus with the observed status of the volume groups on the node where it is running.

## Glob device selectors

Device selectors accept literal paths and shell-style glob patterns prefixed with
`glob:` in both `paths` and `optionalPaths`:

```yaml
spec:
  storage:
    deviceClasses:
      - name: vg1
        deviceSelector:
          paths:
            - "glob:/dev/sd*"
```

This selects all matching paths on each node. Supported wildcards are `*`, `?`,
and character classes such as `[a-z]`. `*` does not cross directory separators;
patterns use filesystem glob syntax, rather than regular expressions. Paths
without `glob:` retain literal matching. Stable identifiers can also be matched,
for example `glob:/dev/disk/by-id/wwn-*`; symlinks resolve to their kernel device
paths, and multiple aliases of the same device are deduplicated.

Each required pattern in `paths` must match at least one usable device or a
device already in the volume group. LVMS selects all matching devices that pass
the existing safety filters and excludes the others. For example, if a required
`glob:/dev/sd*` also matches a disk with an operating-system filesystem signature,
LVMS excludes that disk
and can use the other eligible matches. Patterns in `optionalPaths` may match no
eligible devices. An initial volume group still requires at least one eligible
device. Malformed patterns and patterns outside `/dev/` are rejected by
admission validation.

For glob selectors, LVMS excludes LVM signatures that it cannot identify in the
host's physical-volume inventory, as well as unassigned physical volumes. List a
device explicitly only after confirming that reusing it is safe.

Globs are expanded once per reconciliation for filtering and required-device
checks. Device removal also checks existing PV names directly and stops when
their identity or alias selection cannot be determined. The stored selector
retains the pattern. Glob selectors are checked every 30 seconds to discover newly
attached matching devices, regardless of the discovery policy. Glob selectors are
not supported with RAID; specify literal paths for RAID device classes.

If an optional glob in an alias directory cannot be resolved while existing PVs
are present, LVMS stops device removal rather than assuming the PVs are no
longer selected. Restore the aliases or update the selector to proceed.

Glob selectors can be combined with force wiping. LVMS checks the matching block
devices before wiping: it excludes mounted devices, active swap, devices with
children, LVM or RAID signatures, devices owned by a VG, and devices whose
mount state cannot be verified. Each eligible device is rediscovered and checked
again immediately before wiping, including its serial number and WWN when present.
Without either identifier, the remaining identity checks cannot reliably detect a
replacement device with the same path, device number, and size. The one-shot marker
is persisted before the first wipe, so a crash or
failure requires manual review rather than automatically repeating the wipe.
These checks cannot establish whether an unmounted disk contains data the user
wants to keep or detect every possible use by another host process.
Before wiping, LVMS checks physical-volume ownership, including volume groups
outside LVMS. A required match belonging to another volume group stops the wipe;
an optional match is skipped. A disk containing an owned physical volume is
protected as well, and devices already in the target volume group are not wiped.

## Deletion

A controller owner reference is set on the daemon set, so it is cleaned up when the LVMCluster CR is deleted.

## Considerations

Storing the lvmd config file on the host provides a superior solution when compared to other options:
- Single config map: The process of storing the configuration file contents of lvmd across multiple nodes in a single config map requires additional processing to segment the configuration based on each individual node and store it accordingly before it can be consumed by lvmd.
- Multiple config maps: Although technically possible, using multiple config maps to store lvmd config file contents across nodes would limit the deployment of TopoLVM node plugin as a daemon set. This is because each daemon set requires a unique config map.
