# The Volume Group Manager

The Volume Group Manager manages a single controller/reconciler, which runs as `vg-manager` daemon set pods on a cluster. They are responsible for performing on-node operations for the node they are running on. They first identify disks that match the filters specified for the node. Next, they watch for the LVMVolumeGroup resource and create the necessary volume groups and thin pools on the node based on the specified deviceSelector and nodeSelector. Once the volume groups are created, vg-manager generates the `lvmd.yaml` configuration file for lvmd to use. Additionally, vg-manager updates the LVMVolumeGroupNodeStatus with the observed status of the volume groups on the node where it is running.

## Device path patterns

Both `deviceSelector.paths` and `deviceSelector.optionalPaths` accept entries prefixed
with `glob:`. The remainder is a shell-style pattern (Go's `path/filepath.Match`) and
must start with `/dev/`. Patterns match the entire kernel device name (`KNAME` from
`lsblk`). For example:

```yaml
deviceSelector:
  paths:
    - /dev/disk/by-id/data-disk
    - glob:/dev/nvme[1-3]n1
  optionalPaths:
    - glob:/dev/sd[b-d]
```

Because the syntax is the shell's, `ls <pattern>` on the node previews the candidate
paths — not the final selection. LVMS matches kernel names rather than the symlinks
`ls` prints, then applies the filter chain, deduplication and the ancestor/descendant
rule described below, so the selected set can be smaller than the listing. `*` and `?`
never cross a `/`, and `.` is a literal dot.

Regular expressions are deliberately not supported. `/dev/nvme2*` as a regular
expression means "`/dev/nvme` followed by zero or more `2`s" and silently matches no
real device, while the expression that does work — `/dev/sd.*` — is visually
indistinguishable from the one that matches nothing. On a field that can feed
`wipefs`, the safe-looking spelling must not be the broken one. See
[ADR 0013](../decisions/0013-glob-device-selectors.md).

Literal entries retain symlink resolution and exact matching. Patterns do not expand
symlinks or match `/dev/disk/by-id/` aliases; use the corresponding kernel names
instead. No prefix means a literal path, even if it contains pattern characters.

Each required pattern must match at least one device on every selected node, and all
of its matches must be usable or already part of the volume group. Optional patterns
may match nothing; matching optional devices still pass through the normal filters.
Duplicates across patterns and literal entries are added or wiped only once.

When a pattern matches both a device and one of its own descendants, only one half of
the pair is used, because a disk and its partition can never both become physical
volumes. Which half depends on the operation: volume group membership keeps the
innermost match (a device with children is never a valid PV), while force wiping keeps
the outermost match (wiping a disk already takes its partitions with it).

Force wiping still requires explicit opt-in and respects the per-node wipe annotation;
new matches discovered after that annotation is set are not automatically wiped. Every
selector is resolved before the first `wipefs` call, so a pattern that has not matched
yet cannot leave a node half-wiped with its annotation unwritten.

Invalid patterns are rejected on create and update, and re-checked on the node for
LVMVolumeGroups created by hand, which bypass the LVMCluster webhook.

Patterns are rejected outright in a device class with a `raidConfig`. RAID needs an exact
device count, which a pattern cannot supply at admission, and RAID geometry is fixed when
the logical volume is created, so a device discovered later could not join the array in
any case. List the devices explicitly for RAID. This restriction can be relaxed later
without breaking existing resources; adding it later could not.

Admission rejects any two selectors that can claim the same device, whether they are
literal paths, patterns, or a mix — across device classes and within a single one.
Pattern against pattern is decided by [`internal/glob`](../../internal/glob), which
answers whether two patterns can match a common name without needing to know a node's
devices: `glob:/dev/sd*` and `glob:/dev/*1` are rejected because both match
`/dev/sda1`, while `glob:/dev/sd[a-c]` and `glob:/dev/sd[d-f]` are accepted.

Overlap only matters between classes that can land on the same node, so a pair involving
a pattern is skipped only when their `nodeSelector`s are *provably* disjoint — the same
label key restricted to values that cannot both hold, which is how nodes get split by
zone, rack, role or name. Anything short of a proof is checked, because `host in (a)` and
`zone in (z1)` can both select one node, and node labels change over time. See
[`internal/nodeselector`](../../internal/nodeselector).

Two *literal* paths keep the weaker rule that predates patterns: they are compared only
when their `nodeSelector`s are textually identical. Role labels use `Exists`, which does
not prove two selectors disjoint, so applying the stricter rule to literals would reject
configurations earlier releases accepted — and would block every later edit to those
clusters, not just the device selector.

At runtime the physical volume ownership filter remains the backstop for anything
admission could not see, such as two different symlinks resolving to the same kernel
device, or an LVMVolumeGroup created by hand.

Pattern selectors are refreshed every 30 seconds, regardless of `deviceDiscoveryPolicy`,
because a disk appearing on a node is not a Kubernetes event and polling is the only way
to notice it. A newly matching disk joins the volume group if it passes the filter chain.
Literal-only selectors need no periodic discovery, since their device set cannot change
without an edit to the resource.

Two consequences are worth knowing. A disk that appears after the per-node wipe marker is
set is never wiped, so if it carries a leftover signature the filter chain excludes it —
and because every match of a required pattern must be usable, the volume group fails.
Status also reports `RuntimeDynamic` for any selector holding a pattern, including one
that mixes literal paths with patterns, because a single pattern keeps discovering
devices. Only literal-only selectors report `Preconfigured`.

Narrowing or deleting a pattern requests removal of PVs that no longer match any
required or optional entry. The usual removal guards still apply: missing PVs block
removal, at least one PV must remain, RAID minimum counts are enforced, and LVM must
allow the reduction. A PV reported through a symlink is resolved to its kernel name
before testing pattern membership; unresolved PV aliases block removal.

Membership is tested against the PVs currently in the volume group, so a required
pattern that matches none of them is not an error during removal — it is a pattern
selecting a device that is about to be added. Swapping disks by narrowing one pattern
and adding another in the same update therefore works, subject to those guards.

## Deletion

A controller owner reference is set on the daemon set, so it is cleaned up when the LVMCluster CR is deleted.

## Considerations

Storing the lvmd config file on the host provides a superior solution when compared to other options:
- Single config map: The process of storing the configuration file contents of lvmd across multiple nodes in a single config map requires additional processing to segment the configuration based on each individual node and store it accordingly before it can be consumed by lvmd.
- Multiple config maps: Although technically possible, using multiple config maps to store lvmd config file contents across nodes would limit the deployment of TopoLVM node plugin as a daemon set. This is because each daemon set requires a unique config map.
