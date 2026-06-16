# Known Limitations

## Dynamic Device Discovery

When a `DeviceSelector` isn't configured for a device class, LVMS operates dynamically, continuously monitoring attached devices on the node and adding them to the volume group if they're unused and supported. However, this approach presents several potential issues:

- LVMS may inadvertently add a device to the volume group that wasn't intended for LVMS.
- Removing devices could disrupt the volume group.
- LVMS lacks awareness of volume group changes that could lead to data loss, potentially necessitating manual node remediation.

Given these considerations, it's advised against using LVMS in dynamic discovery mode for production environments.

## Unsupported Device Types

Here is a list of the types of devices that are excluded by LVMS. To get more information about the devices on your machine and to check if they fall under any of these filters, run:

```bash
$ lsblk --paths --json -o NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,STATE,KNAME,SERIAL,PARTLABEL,FSTYPE
```

1. **Read-Only Devices:**
    - *Condition:* Devices marked as `read-only` are unsupported.
    - *Why:* LVMS requires the ability to write and modify data dynamically, which is not possible with devices set to read-only mode.
    - *Filter:* `ro` is set to `true`.

2. **Suspended Devices:**
    - *Condition:* Devices in a `suspended` state are unsupported.
    - *Why:* A suspended state implies that a device is temporarily inactive or halted, and attempting to incorporate such devices into LVMS can introduce complexities and potential issues.
    - *Filter:* `state` is `suspended`.

3. **Devices with Invalid Partition Labels:**
    - *Condition:* Devices with partition labels such as `bios`, `boot`, or `reserved` are unsupported.
    - *Why:* These labels indicate reserved or specialized functionality associated with specific system components. Attempting to use such devices within LVMS may lead to unintended consequences, as these labels may be reserved for system-related activities.
    - *Filter:* `partlabel` has either `bios`, `boot`, or `reserved`.

4. **Devices with Invalid Filesystem Signatures:**
    - *Condition:* Devices with invalid filesystem signatures are unsupported. This includes:
        - Devices with a filesystem type set to `LVM2_member` (only valid if no children).
        - Devices with no free capacity as a physical volume.
        - Devices already part of another volume group.
    - *Why:* These conditions indicate that either this device is already used by another volume group or have no free capacity to be used within LVMS.
    - *Filter:* `fstype` is not `null`, or `fstype` is set to `LVM2_member` and has children block devices, or `pvs --units g -v --reportformat json` returns `pv_free` for the block device set to `0G`.

5. **Devices with Children:**
    - *Condition:* Devices with children block devices are unsupported.
    - *Why:* LVMS operates optimally with standalone block devices that are not part of a hierarchical structure. Devices with children can complicate volume management, potentially causing conflicts, errors, or difficulties in tracking and managing logical volumes.
    - *Filter:* `children` has children block devices.

6. **Devices with Bind Mounts:**
    - *Condition:* Devices with bind mounts are unsupported.
    - *Why:* Managing logical volumes becomes more complex when dealing with devices that have bind mounts, potentially causing conflicts or difficulties in maintaining the integrity of the logical volume setup.
    - *Filter:* `cat /proc/1/mountinfo | grep <device-name>` returns mount points for the device in the 4th or 10th field.

7. **ROM Devices:**
    - *Condition:* Devices of type `rom` are unsupported.
    - *Why:* Such devices are designed for static data storage and lack the necessary read-write capabilities essential for dynamic operations performed by LVMS.
    - *Filter:* `type` is set to `rom`.

8. **LVM Partitions:**
    - *Condition:* Devices of type `LVM` partition are unsupported.
    - *Why:* These partitions are already dedicated to LVM and are managed as part of an existing volume group.
    - *Filter:* `type` is set to `lvm`.

9. **Loop Devices:**
    - *Condition:* Loop Devices must not be used if they are already in use by Kubernetes.
    - *Why:* When loop devices are utilized by Kubernetes, they are likely configured for specific tasks or processes managed by the Kubernetes environment. Integrating loop devices that are already in use by Kubernetes into LVMS can lead to potential conflicts and interference with the Kubernetes system.
    - *Filter:* `type` is set to `loop`, and `losetup <loop-device> -O BACK-FILE --json` returns a `back-file` which contains `plugins/kubernetes.io`.

Devices meeting any of these conditions are filtered out for LVMS operations.

_NOTE: It is strongly recommended to perform a thorough wipe of a device before using it within LVMS to proactively prevent unintended behaviors or potential issues._

## Single LVMCluster Support

LVMS does not support the reconciliation of multiple LVMCluster custom resources simultaneously.

## RAID and Thin Provisioning Are Mutually Exclusive

LVMS supports native LVM RAID through the `raidConfig` field on a device class (RAID1, 4, 5, 6, and 10 are supported). When `raidConfig` is set, the device class uses **thick provisioning** — RAID and thin provisioning are mutually exclusive within a single device class.

This means **RAID device classes do not support snapshots or clones**. If you need both redundancy and snapshot/clone capability, use [`mdraid`](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/managing_storage_devices/managing-raid_managing-storage-devices#linux-raid-subsystems_managing-raid) at the OS level: create a RAID array with `mdadm`, then reference the resulting device (e.g. `/dev/md0`) in the `deviceSelector` of a thin-provisioned device class.

_NOTE: `mdraid` devices are not automatically discovered — they must be listed explicitly in `deviceSelector`._

## Missing LV-Level Encryption Support

Currently, LVM Operator does not have a native LV-level encryption support. Instead, you can encrypt the entire disk or partitions, and use them within LVMCluster. This way all LVs created by LVMS on this disk will be encrypted out-of-the-box.

Here is an example `MachineConfig` that can be used to configure encrypted partitions during an OpenShift installation:

```yaml
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: 98-encrypted-disk-partition-master
  labels:
    machineconfiguration.openshift.io/role: master
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      disks:
        - device: /dev/nvme0n1
          wipeTable: false
          partitions:
            - sizeMiB: 204800
              startMiB: 600000
              label: application
              number: 5
      luks:
        - clevis:
            tpm2: true
          device: /dev/disk/by-partlabel/application
          name: application
          options:
          - --cipher
          - aes-cbc-essiv:sha256
          wipeVolume: true
```

Then, the path to the encrypted partition `/dev/mapper/application` can be specified in the `deviceSelector`.

For non-OpenShift clusters, you can encrypt a disk using LUKS with `cryptsetup`, and then use this in your `deviceSelector` within `LVMCluster`:

1. Set up the `/dev/sdb` device for encryption. This will also remove all the data on the device:

   ```bash
   cryptsetup -y -v luksFormat /dev/sdb
   ```

    You'll be prompted to set a passphrase to unlock the volume.

2. Create a logical device-mapper device named `encrypted`, mounted to the LUKS-encrypted device:

   ```bash
   cryptsetup luksOpen /dev/sdb encrypted
   ```

    You'll be prompted to enter the passphrase you set when creating the volume.

3. You can now reference `/dev/mapper/encrypted` in the `deviceSelector`.

## Snapshotting and Cloning in Multi-Node Topologies

In general, since LVMCluster does not ensure data replication, `VolumeSnapshots` and consumption of them is always limited to the original dataSource.
Thus, snapshots must be created on the same node as the original data. Also, all pods relying on a PVC that is using the snapshot data will have to be scheduled
on the node that contained the original `LogicalVolume` in TopoLVM.

It should be noted that snapshotting is based on Thin-Pool Snapshots from upstream TopoLVM and are still considered [experimental in upstream](https://github.com/topolvm/topolvm/discussions/737).
This is because multi-node Kubernetes clusters have the scheduler figure out pod placement logically onto different nodes (with the node topology from the native Kubernetes Scheduler responsible for deciding the node where Pods should be deployed),
and it cannot always be guaranteed that Snapshots are provisioned on the same node as the original data (which is based on the CSI topology, known by TopoLVM) if the `PersistentVolumeClaim` is not created upfront.

If you are unsure what to make of this, always make sure that the original `PersistentVolumeClaim` that you want to have Snapshots on is already created and `Bound`.
With these prerequisites it can be guaranteed that all follow-up `VolumeSnapshot` Objects as well as `PersistentVolumeClaim` objects depending on the original one are scheduled correctly.
The easiest way to achieve this is to use precreated `PersistentVolumeClaims` and non-ephemeral `StatefulSet` for your workload.

_NOTE: All of the above also applies for cloning the `PersistentVolumeClaims` directly by using the original `PersistentVolumeClaims` as data source instead of using a Snapshot._

## Validation of `LVMCluster` CRs Outside the `openshift-lvm-storage` Namespace

When creating an `LVMCluster` CR outside the `openshift-lvm-storage` namespace by installing it via `ClusterServiceVersion`, the Operator will not be able to validate the CR.
This is because the `ValidatingWebhookConfiguration` is restricted to the `openshift-lvm-storage` namespace and does not have access to the `LVMCluster` CRs in other namespaces.
Thus, the Operator will not be able to prevent the creation of invalid `LVMCluster` CRs outside the `openshift-lvm-storage` namespace.
However, it will also not pick it up and simply ignore it.

This is because Operator Lifecycle Manager (OLM) does not allow the creation of `ClusterServiceVersion` with installMode `OwnNamespace` while also not restricting the webhook configuration.
Validation in the `openshift-lvm-storage` namespace is processed normally.
