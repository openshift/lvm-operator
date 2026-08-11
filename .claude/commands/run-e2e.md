# E2E Test Execution

## Prerequisites

1. Verify cluster access:
```bash
oc whoami
oc get nodes
```

2. Check for available block devices or set up loop devices:
```bash
lsblk
```

If no spare block devices, see [docs/loop-devices.md](../../docs/loop-devices.md) for loop device setup.

## Deploy

```bash
make deploy
```

Wait for the operator pod to be ready:
```bash
oc get pods -n openshift-lvm-storage -w
```

## Run Tests

```bash
make e2e
```

## Validation Order

E2E tests validate in reconciliation order:
1. LVMCluster CR created and conditions progressing
2. CSI Driver registered
3. CSINodeInfo populated
4. VG Manager DaemonSet ready on target nodes
5. LVMVolumeGroup status reflects volume groups
6. StorageClass created with correct parameters
7. VolumeSnapshotClass created (if snapshot testing enabled)

## Cleanup

```bash
make undeploy
```

If LVMCluster is stuck deleting, check finalizers:
```bash
oc get lvmcluster -o yaml | grep finalizers -A 5
```
