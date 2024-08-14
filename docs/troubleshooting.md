# Troubleshooting Guide

## Persistent Volume Claim (PVC) is stuck in `Pending` state

There can be many reasons why a `PersistentVolumeClaim` (`PVC`) gets stuck in a `Pending` state. Some possible reasons and some troubleshooting suggestions are described below.

 ```bash
 $ oc get pvc
 NAME        STATUS    VOLUME   CAPACITY   ACCESS MODES   STORAGECLASS   AGE
 lvms-test   Pending                                      lvms-vg1       11s
 ```

To troubleshoot the issue, inspect the events associated with the PVC. These events can provide valuable insights into any errors or issues encountered during the provisioning process.

 ```bash
 $ oc describe pvc lvms-test
 Events:
   Type     Reason              Age               From                         Message
   ----     ------              ----              ----                         -------
   Warning  ProvisioningFailed  4s (x2 over 17s)  persistentvolume-controller  storageclass.storage.k8s.io "lvms-vg1" not found
 ```

### `LVMCluster` CR or the Logical Volume Manager Storage (LVMS) components are missing

If you encounter a `storageclass.storage.k8s.io 'lvms-vg1' not found` error, verify the presence of the `LVMCluster` resource:

 ```bash
 $ oc get lvmcluster -n openshift-storage
 NAME            AGE
 my-lvmcluster   65m
 ```

If an `LVMCluster` resource is not found, you can create one. See [samples/lvm_v1alpha1_lvmcluster.yaml](../config/samples/lvm_v1alpha1_lvmcluster.yaml) for an example CR that you can modify.

```bash
$ oc create -n openshift-storage -f https://github.com/openshift/lvm-operator/raw/main/config/samples/lvm_v1alpha1_lvmcluster.yaml
```

If an `LVMCluster` already exists, check if all the pods from LVMS are in the `Running` state in the `openshift-storage` namespace:

 ```bash
 $ oc get pods -n openshift-storage
 NAME                                  READY   STATUS    RESTARTS      AGE
 lvms-operator-7b9fb858cb-6nsml        1/1     Running   0             70m
 vg-manager-r6zdv                      1/1     Running   0             66m
 ```

There should be one running instance of `lvms-operator` and `vg-manager` (per Node).

#### `vg-manager` is stuck in CrashLoopBackOff state

This error indicates a failure in locating an available disk for LVMS utilization. To investigate further and obtain relevant information, review the status of the `LVMCluster` CR:

```bash
$ oc describe lvmcluster -n openshift-storage
```

If you encounter a failure message such as `no available devices found` while inspecting the status, establish a direct connection to the host where the problem is occurring. From there, run:

```bash
$ lsblk --paths --json -o NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,STATE,KNAME,SERIAL,PARTLABEL,FSTYPE
```

This prints information about the disks on the host. Review this information to see why a device is not considered available for LVMS utilization. For example, if a device has partlabel `bios` or `reserved`, or if they are suspended or read-only, or if they have children disks or `fstype` set, LVMS considers them unavailable. Check [filter.go](../internal/controllers/vgmanager/filter/filter.go) for the complete list of filters LVMS makes use of.

> If you set a device path in the `LVMCluster` CR under `spec.storage.deviceClasses.deviceSelector.paths`, make sure the paths match with `kname` of the device from the `lsblk` output.

You can also review the logs of the `vg-manager` pod to see if there are any further issues:

```bash
$ oc logs -l app.kubernetes.io/name=vg-manager -n openshift-storage
```

### Disk failure

If you encounter a failure message such as `failed to check volume existence` while inspecting the events associated with the `PVC`, it might indicate a potential issue related to the underlying volume or disk. This failure message suggests that there is problem with the availability or accessibility of the specified volume. Further investigation is recommended to identify the exact cause and resolve the underlying issue.

```bash
$ oc describe pvc lvms-test
Events:
Type     Reason              Age               From                         Message
----     ------              ----              ----                         -------
Warning  ProvisioningFailed  4s (x2 over 17s)  persistentvolume-controller  failed to provision volume with StorageClass "lvms-vg1": rpc error: code = Internal desc = failed to check volume existence
```

To investigate the issue further, establish a direct connection to the host where the problem is occurring. From there, create a new file on the disk. This will help you to see the underlying problem related to the disk. After resolving the underlying disk problem, if the recurring issue persists despite the resolution, it may be necessary to perform a [forced cleanup procedure](#forced-cleanup) for LVMS. After completing the cleanup process, re-create the `LVMCluster` CR. When you re-create the `LVMCluster` CR, all associated objects and resources are recreated, providing a clean starting point for the LVMS deployment. This helps to ensure a reliable and consistent environment.

### Node failure

If PVCs associated with a specific node remain in a `Pending` state, it suggests a potential issue with that particular node. To identify the problematic node, you can examine the restart count of the `topolvm-node` pod. An increased restart count indicates potential problems with the underlying node, which may require further investigation and troubleshooting.

 ```bash
 $ oc get pods -n openshift-storage
 NAME                                  READY   STATUS    RESTARTS      AGE
 lvms-operator-7b9fb858cb-6nsml        3/3     Running   0             70m
 topolvm-controller-5dd9cf78b5-7wwr2   5/5     Running   0             66m
 topolvm-node-dr26h                    4/4     Running   0             66m
 topolvm-node-54as8                    4/4     Running   0             66m
 topolvm-node-78fft                    4/4     Running   17 (8s ago)   66m
 vg-manager-r6zdv                      1/1     Running   0             66m
 vg-manager-990ut                      1/1     Running   0             66m
 vg-manager-an118                      1/1     Running   0             66m
 ```

After resolving the issue with the respective node, if the problem persists and reoccurs, it may be necessary to perform a [forced cleanup procedure](#forced-cleanup) for LVMS. After completing the cleanup process, re-create the LVMCluster. By re-creating the LVMCluster, all associated objects and resources are recreated, providing a clean starting point for the LVMS deployment. This helps to ensure a reliable and consistent environment.

## Forced cleanup

After resolving any disk or node related problem, if the recurring issue persists despite the resolution, it may be necessary to perform a forced cleanup procedure for LVMS. This procedure aims to comprehensively address any persistent issues and ensure the proper functioning of the LVMS solution.

1. Remove all the PVCs created using LVMS, and pods using those PVCs.
2. Switch to `openshift-storage` namespace:

    ```bash
    $ oc project openshift-storage
    ```

3. Make sure there are no `LogicalVolume` resources left:

    ```bash
    $ oc get logicalvolume
    No resources found
    ```

    If there are `LogicalVolume` resources present, remove finalizers from the resources and delete them:

    ```bash
    oc patch logicalvolume <name> -p '{"metadata":{"finalizers":[]}}' --type=merge
    oc delete logicalvolume <name>
    ```

4. Make sure there is no `LVMVolumeGroup` resources left:

    ```bash
    $ oc get lvmvolumegroup
    No resources found
    ```

    If there is any `LVMVolumeGroup` resources left, remove finalizers from these resources and delete them:

    ```bash
    $ oc patch lvmvolumegroup <name> -p '{"metadata":{"finalizers":[]}}' --type=merge
    $ oc delete lvmvolumegroup <name>
    ```

5. Remove any `LVMVolumeGroupNodeStatus` resources:

    ```bash
    $ oc delete lvmvolumegroupnodestatus --all
    ```

6. Remove the `LVMCluster` resource:

    ```bash
    oc delete lvmcluster --all
    ```

## Graceful cleanup with partial recovery of data

In some situations it might be undesirable to perform a forced cleanup, as it may result in data loss of all data in the LVMCluster. In such cases, you can perform a graceful cleanup with partial recovery of data. This procedure aims to address the issue while preserving the data associated with the PVCs that are not affected. This is most likely to happen in a multi-node environment, but sometimes can also help in single-node situations where one or more, but not all included disks of the LVMCluster are affected.

### Steps to perform a graceful cleanup with partial recovery of data on a single node

In case of a failing / infinitely progressing LVMCluster, you can try to perform a graceful cleanup when:
1. The volume group in lvm2 is still recognizable on the host system and the disks are still accessible.
2. The `vg-manager` pod is stuck in a `CrashLoopBackOff` state and the LVMCluster is in a continous Failed or Progressing state.

#### Recovery from disk failure without resetting LVMCluster

LVMCluster and its node-daemon vg-manager periodically reconcile changes on the node and attempt to recreate the Volume Group and Thin Pool (if necessary) on the node. If the Volume Group is still recognizable on the host system and the disks are still accessible, you can try to recover from the failure without resetting the LVMCluster by following the normal recovery process in lvm.

1. Login to the node where the `vg-manager` pod is stuck in a `CrashLoopBackOff` state.
2. Check the status of the Volume Group:

    ```bash
    $ vgs <YOUR_VG_NAME_HERE> -o all --reportformat json --units g | jq .report[0].vg[0]
    ```

    If the Volume Group is still recognizable, you can try to recover from the failure based on its returned attributes:

    Here is a healthy example of a volume group:

    ```json
   {
      "vg_fmt": "lvm2",
      "vg_uuid": "mb1eRp-0x1O-jdJc-bFPN-tB2o-poS5-nOnmPi",
      "vg_name": "vg1",
      "vg_attr": "wz--n-",
      "vg_permissions": "writeable",
      "vg_extendable": "extendable",
      "vg_exported": "",
      "vg_autoactivation": "enabled",
      "vg_partial": "",
      "vg_allocation_policy": "normal",
      "vg_clustered": "",
      "vg_shared": "",
      "vg_size": "0.97g",
      "vg_free": "0.97g",
      "vg_sysid": "",
      "vg_systemid": "",
      "vg_lock_type": "",
      "vg_lock_args": "",
      "vg_extent_size": "0.00g",
      "vg_extent_count": "249",
      "vg_free_count": "249",
      "max_lv": "0",
      "max_pv": "0",
      "pv_count": "1",
      "vg_missing_pv_count": "0",
      "lv_count": "0",
      "snap_count": "0",
      "vg_seqno": "1",
      "vg_tags": "",
      "vg_profile": "",
      "vg_mda_count": "1",
      "vg_mda_used_count": "1",
      "vg_mda_free": "0.00g",
      "vg_mda_size": "0.00g",
      "vg_mda_copies": "unmanaged"
   }
   ```

   There are quite a few fields available that give hints on possible issues. The most important ones are the
   - `vg_attr` field, which should be `wz--n-` for a normal volume group. This indicates a normal writeable, zeroing volume group
     Any deviation from this might indicate a problem with the volume group.
   - `vg_free` field, which should be (significantly) greater than 1. This indicates the amount of free space (in Gi) in the volume group.
     If this is 0, the volume group is full (or nearly full with less than 1 Gi available) and no more logical volumes can be created now or in the near future
   - `vg_missing_pv_count` field, which should be 0. This indicates the number of missing physical volumes in the volume group. If this is greater than 0, the volume group is incomplete and might not be able to function properly.

3. Allow erasure of missing volumes in the volume group (if necessary due to `vg_missing_pv_count` > 0):
   If the volume group consisted of multiple physical volumes, it might be that one or more of them are missing, but there are still healthy volumes left. In this case, you can remove the missing volumes from the volume group to allow the volume group to function properly again.
   This will completely erode any potential of automatic recovery on the failing disk, but allows you to activate the volume group again,
   with all the remaining disks and data on these disks.

   Note that it is advisable to deschedule all workloads using the storage class backed by the volume group, and then call
   ```bash
   vgchange --activate n <YOUR_VG_NAME_HERE>
   ```
   This will make sure that no active writes can occur on the volume group while you are erasing the missing volumes.
   Sometimes, lvm2 will prohibit you from erasing the missing volumes before deactivating the volumes in the volume group.

   Once you have confirmed a disk failure, you can erase the missing volumes from the volume group by calling

   ```bash
   vgreduce --removemissing <YOUR_VG_NAME_HERE>
   ```

   If all goes well, another verification of the volume group should confirm that the `vg_missing_pv_count` is now 0.
   This means that the existing data on the remaining disks is still accessible and the volume group can be activated again.

   At this point, it is safe to reschedule your workloads and continue using the volume group.
   However, the LVMCluster might still be failing, as the Volume Group is expected to contain the now missing device.
   This can happen when the LVMCluster was created with a deviceSelector in the failing deviceClass and the DeviceDiscoveryPolicy is set to Preconfigured.

   To fix this, change the lvmcluster with the following patch:

   ```bash
   oc patch LVMCluster my-lvmcluster --type='json' -p='[{"op": "remove", "path": "/spec/storage/deviceClasses/0/deviceSelector/paths/0"}]'
   ```

   This might be rejected in case the LVMClusters deviceSelector is static. In this case it is necessary to temporarily disable the ValidatingWebhook that normally ensures that the deviceSelector is valid.

    ```bash
    oc patch validatingwebhookconfigurations.admissionregistration.k8s.io lvm-operator-webhook --type='json' -p='[{"op": "replace", "path": "/webhooks/0/failurePolicy", "value": "Ignore"}]'
    ```

   After you are done with the patch, you can reenable the webhook by calling

    ```bash
    oc patch validatingwebhookconfigurations.admissionregistration.k8s.io lvm-operator-webhook --type='json' -p='[{"op": "replace", "path": "/webhooks/0/failurePolicy", "value": "Fail"}]'
    ```

   After this, you can wait for vg-manager / LVMCluster to reconcile the changes, and the LVMCluster should be healthy again. (of course this time without the failing disk)


### Steps to perform a graceful cleanup with partial recovery of data after node failure (multiple nodes)

In case of a failing / infinitely progressing LVMCluster due to one or many nodes failing, you can try to perform a graceful cleanup when:
1. The volume group in lvm2 is still recognizable on the host system and the disks are still accessible on at least one of the nodes selected in the NodeSelector of the LVMCluster.
2. The `vg-manager` pod is stuck in a `CrashLoopBackOff` state and the LVMCluster is in a continous Failed or Progressing state on all but that accessible one node.

In other words: Make sure there is at least one node available with a running `vg-manager` pod and a healthy volume group node status.

#### Recovery from node failure without resetting LVMCluster

LVMCluster and its node-daemon vg-manager periodically reconcile changes on the node and attempt to recreate the Volume Group and Thin Pool (if necessary) on the node.
If the Volume Group is still recognizable on the host system and the disks are still accessible on at least one node, you can follow the following procedure to reduce the LVMCluster to only the healthy node(s) and recover from the failure without resetting the LVMCluster.

1. Modify the nodeSelector of the affected deviceClass to only include the healthy node(s) with the healthy volume group node status.
   ```bash
   oc patch lvmcluster <MY_LVMCLUSTER_NAME_HERE> --type='json' -p='[{"op": "replace", "path": "/spec/storage/deviceClasses/0/nodeSelector", "value": {nodeSelectorTerms: [{matchExpressions: [{key: "kubernetes.io/hostname", operator: "In", values: ["<HEALTHY_NODE_NAME_HERE>"]}]}]}}]'}]'
   ```
   This will remove the failing node(s) from the LVMCluster and only include the healthy node(s) in the LVMCluster.

   It might be necessary to temporarily disable the ValidatingWebhook that normally ensures that the deviceSelector is valid.

   ```bash
   oc patch validatingwebhookconfigurations.admissionregistration.k8s.io <LVM_CLUSTER_WEBHOOK_HERE> --type='json' -p='[{"op": "replace", "path": "/webhooks/0/failurePolicy", "value": "Ignore"}]'
   ```

    After you are done with the patch, you can reenable the webhook by calling

   ```bash
   oc patch validatingwebhookconfigurations.admissionregistration.k8s.io <LVM_CLUSTER_WEBHOOK_HERE> --type='json' -p='[{"op": "replace", "path": "/webhooks/0/failurePolicy", "value": "Fail"}]'
   ```

2. Wait for the LVMCluster to reconcile the changes. The LVMCluster should now only contain the healthy node(s) and the failing node(s) should be removed from the LVMCluster. The LVMCluster should now be Ready again.
   Note that now pods using the deviceClass / StorageClass backed by the deviceClass will only be scheduled on the healthy node(s) and the failing node(s) will not be used / usable anymore.
   It is thus recommended to use a different deviceClass for the failing node(s) if you want to use them again in the future and move workloads over after recovering their data.
   If the node failure was temporary, you can use the same mechanism as described in the Recovery from disk failure without resetting LVMCluster section to reenable the failing node(s) in the LVMCluster by changing the nodeSelector back to include the failing node(s) again.
