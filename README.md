# The LVM Operator - part of LVMS

## [Official LVMS Product Documentation](https://docs.openshift.com/container-platform/latest/storage/persistent_storage/persistent_storage_local/persistent-storage-using-lvms.html)

For the latest information about usage and installation of LVMS (Logical Volume Manager Storage) in OpenShift, please use the official product documentation linked above.

## Overview

Use the LVM Operator with `LVMCluster` custom resources to deploy and manage LVM storage on OpenShift clusters.

The LVM Operator leverages the [TopoLVM CSI Driver](https://github.com/topolvm/topolvm) on the backend to dynamically create LVM physical volumes, volume groups and logical volumes, and binds them to `PersistentVolumeClaim` resources.
This allows applications running on the cluster to consume storage from LVM logical volumes backed by the TopoLVM CSI Driver.

The LVM Operator, in conjunction with the TopoLVM CSI Driver, Volume Group Manager, and other related components, collectively comprise the Logical Volume Manager Storage (LVMS) solution.

Here is a brief overview of how the Operator works. See the [architecture overview](docs/architecture.md) for design rationale, component diagrams, and deployment topology.

```mermaid
graph LR
LVMOperator((LVMOperator))-->|Manages| LVMCluster
LVMOperator-->|Manages| StorageClass
StorageClass-->|Creates| PersistentVolumeA
StorageClass-->|Creates| PersistentVolumeB
PersistentVolumeA-->LV1
PersistentVolumeB-->LV2
LVMCluster-->|Comprised of|Disk1((Disk1))
LVMCluster-->|Comprised of|Disk2((Disk2))
LVMCluster-->|Comprised of|Disk3((Disk3))

subgraph Logical Volume Manager
  Disk1-->|Abstracted|PV1
  Disk2-->|Abstracted|PV2
  Disk3-->|Abstracted|PV3
  PV1-->VG
  PV2-->VG
  PV3-->VG
  LV1-->VG
  LV2-->VG
end
```

- [Deploying the LVM Operator](#deploying-the-lvm-operator)
    * [Deploying the Operator](#deploying-the-operator)
    * [Inspecting the storage objects on the node](#inspecting-the-storage-objects-on-the-node)
    * [Testing the Operator](#testing-the-operator)
- [Cleanup](#cleanup)
- [Metrics](#metrics)
- [Known Limitations](#known-limitations)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)

## Deploying the LVM Operator

Pre-built catalog images are available and can be used directly — proceed to the [deployment steps](#deploying-the-operator) below. Note that pre-built images may not be in sync with the current state of the repository.

To build the Operator from source instead (including OLM bundle and catalog images), see the [contribution guide](CONTRIBUTING.md#local-builds).

### Deploying the Operator

You can begin the deployment by running the following command:

```bash
$ make deploy
```

<details><summary><strong>Deploying the Operator with OLM</strong></summary>
<p>

You can begin the deployment using the Operator Lifecycle Manager (OLM) by running the following command:

```bash
$ make deploy-with-olm
```

The process involves the creation of several resources to deploy the Operator using OLM. These include a custom `CatalogSource` to define the Operator source, the `openshift-lvm-storage` namespace to contain the Operator components, an `OperatorGroup` to manage the lifecycle of the Operator, a `Subscription` to subscribe to the Operator catalog in the `openshift-lvm-storage` namespace, and finally, the creation of a `ClusterServiceVersion` to describe the Operator's capabilities and requirements.

Wait until the `ClusterServiceVersion` (CSV) reaches the `Succeeded` status:

```bash
$ kubectl get csv -n openshift-lvm-storage

NAME                   DISPLAY       VERSION   REPLACES   PHASE
lvms-operator.v0.0.1   LVM Storage   0.0.1                Succeeded
```

</p>
</details>

After the previous command has completed successfully, switch over to the `openshift-lvm-storage` namespace:

```bash
$ oc project openshift-lvm-storage
```

Wait until all pods have started running:

```bash
$ oc get pods -w
```

Once all pods are running, create a sample `LVMCluster` custom resource (CR):

```bash
$ oc create -n openshift-lvm-storage -f https://github.com/openshift/lvm-operator/raw/main/config/samples/lvm_v1alpha1_lvmcluster.yaml
```

After the CR is deployed, the following actions are executed:

- A Logical Volume Manager (LVM) volume group named `vg1` is created, utilizing all available disks on the cluster.
- A thin pool named `thin-pool-1` is created within `vg1`, with a size equivalent to 90% of `vg1`.
- The TopoLVM Container Storage Interface (CSI) plugin is deployed.
- A storage class and a volume snapshot class are created, both named `lvms-vg1`. This facilitates storage provisioning for OpenShift workloads. The storage class is configured with the `WaitForFirstConsumer` volume binding mode that is utilized in a multi-node configuration to optimize the scheduling of pod placement. This strategy prioritizes the allocation of pods to nodes with the greatest amount of available storage capacity.
- The LVMS system also creates two additional internal CRs to support its functionality:
  * `LVMVolumeGroup` is generated and managed by LVMS to monitor the individual volume groups across multiple nodes in the cluster.
  * `LVMVolumeGroupNodeStatus` is created by the [Volume Group Manager](docs/design/vg-manager.md). This CR is used to monitor the status of volume groups on individual nodes in the cluster.

Wait until the `LVMCluster` reaches the `Ready` status:

```bash
$ oc get lvmclusters.lvm.topolvm.io my-lvmcluster

NAME            STATUS
my-lvmcluster   Ready
```

Wait until all pods are active:

```bash
$ oc get pods -w
```

Once all the pods have been launched, the LVMS is ready to manage your logical volumes and make them available for use in your applications.

### Inspecting the storage objects on the node

Prior to the deployment of the Logical Volume Manager Storage (LVMS), there are no pre-existing LVM physical volumes, volume groups, or logical volumes associated with the disks.

```bash
sh-4.4# lsblk
NAME    MAJ:MIN RM   SIZE RO TYPE MOUNTPOINT
sdb       8:16   0 893.8G  0 disk
|-sdb1    8:17   0     1M  0 part
|-sdb2    8:18   0   127M  0 part
|-sdb3    8:19   0   384M  0 part /boot
`-sdb4    8:20   0 893.3G  0 part /sysroot
sr0      11:0    1   987M  0 rom
nvme0n1 259:0    0   1.5T  0 disk
nvme1n1 259:1    0   1.5T  0 disk
nvme2n1 259:2    0   1.5T  0 disk
sh-4.4# pvs
sh-4.4# vgs
sh-4.4# lvs
```

After successful deployment, the necessary LVM physical volumes, volume groups, and thin pools are created on the host.

```bash
sh-4.4# pvs
  PV           VG  Fmt  Attr PSize  PFree
  /dev/nvme0n1 vg1 lvm2 a--  <1.46t <1.46t
  /dev/nvme1n1 vg1 lvm2 a--  <1.46t <1.46t
  /dev/nvme2n1 vg1 lvm2 a--  <1.46t <1.46t
sh-4.4# vgs
  VG  #PV #LV #SN Attr   VSize  VFree
  vg1   3   0   0 wz--n- <4.37t <4.37t
sh-4.4# lvs
  LV          VG  Attr       LSize  Pool Origin Data%  Meta%  Move Log Cpy%Sync Convert
  thin-pool-1 vg1 twi-a-tz-- <3.93t             0.00   1.19
```

### Testing the Operator

Once you have completed [the deployment steps](#deploying-the-operator), you can proceed to create a basic test application that will consume storage.

To initiate the process, create a Persistent Volume Claim (PVC):

```bash
$ cat <<EOF | oc apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: lvms-test
  labels:
    type: local
spec:
  storageClassName: lvms-vg1
  resources:
    requests:
      storage: 5Gi
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
EOF
```

Upon creation, you may observe that the PVC remains in a `Pending` state.

```bash
$ oc get pvc

NAME        STATUS    VOLUME   CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvms-test   Pending                                      lvms-vg1       7s
```

This behavior is expected as the storage class awaits the creation of a pod that requires the PVC.

To move forward, create a pod that can utilize this PVC:

```bash
$ cat <<EOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: lvms-test
spec:
  volumes:
    - name: storage
      persistentVolumeClaim:
        claimName: lvms-test
  containers:
    - name: container
      image: public.ecr.aws/docker/library/nginx:latest
      ports:
        - containerPort: 80
          name: "http-server"
      volumeMounts:
        - mountPath: "/usr/share/nginx/html"
          name: storage
EOF
```

Once the pod has been created and associated with the corresponding PVC, the PVC is bound, and the pod transitions to the `Running` state.

```bash
$ oc get pvc,pods

NAME                              STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
persistentvolumeclaim/lvms-test   Bound    pvc-a37ef71c-a9b9-45d8-96e8-3b5ad30a84f6   5Gi        RWO            lvms-vg1       3m2s

NAME            READY   STATUS    RESTARTS   AGE
pod/lvms-test   1/1     Running   0          28s
```

For testing without physical disks, you can use loop devices. See the [loop devices guide](docs/loop-devices.md).

## Cleanup

To perform a full cleanup, follow these steps:

1. Remove all the application pods which are using PVCs created with LVMS, and then remove all these PVCs.

2. Ensure that there are no remaining `LogicalVolume` custom resources that were created by LVMS.

    ```bash
    $ oc get logicalvolumes.topolvm.io
    No resources found
    ```

3. Remove the `LVMCluster` CR.

    ```bash
    $ oc delete lvmclusters.lvm.topolvm.io my-lvmcluster
    lvmcluster.lvm.topolvm.io "my-lvmcluster" deleted
    ```

    If the previous command is stuck, it may be necessary to perform a [forced cleanup procedure](./docs/troubleshooting.md#forced-cleanup).

4. Verify that the only remaining resource in the `openshift-lvm-storage` namespace is the Operator.

    ```bash
    oc get pods -n openshift-lvm-storage
    NAME                                 READY   STATUS    RESTARTS   AGE
    lvms-operator-8bf864c85-8zjlp        3/3     Running   0          125m
    ```

5. To begin the undeployment process of LVMS, use the following command:

    ```bash
    make undeploy
    ```

## Metrics

To enable monitoring on OpenShift clusters, assign the `openshift.io/cluster-monitoring` label to the same namespace that you deployed LVMS to.

```bash
$ oc patch namespace/openshift-lvm-storage -p '{"metadata": {"labels": {"openshift.io/cluster-monitoring": "true"}}}'
```

LVMS provides [TopoLVM metrics](https://github.com/topolvm/topolvm/blob/v0.21.0/docs/topolvm-node.md#prometheus-metrics) and `controller-runtime` metrics, which can be accessed via OpenShift Console.

## Known Limitations

Be aware of these limitations when using LVMS. See the [full details](docs/known-limitations.md).

- **Dynamic device discovery** not recommended for production (use explicit device paths)
- **Unsupported device types** — read-only, suspended, ROM, LVM partitions, devices with children/bind mounts/reserved partition labels, and loop devices in use by Kubernetes are filtered out
- **Single LVMCluster** — only one LVMCluster CR is supported per cluster
- **No upgrades from 4.10/4.11** — breaking API changes prevent upgrades
- **No native LVM RAID** — use mdraid instead for redundancy
- **No LV-level encryption** — encrypt disks/partitions before adding to LVMCluster
- **Snapshots/clones limited to single node** — must be on the same node as the source data
- **Webhook validation scoped to operator namespace** — LVMCluster CRs outside `openshift-lvm-storage` are not validated

## Troubleshooting

See the [troubleshooting guide](docs/troubleshooting.md).

## Contributing

See the [contribution guide](CONTRIBUTING.md).
