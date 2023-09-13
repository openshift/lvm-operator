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
 lvms-operator-7b9fb858cb-6nsml        3/3     Running   0             70m
 topolvm-controller-5dd9cf78b5-7wwr2   5/5     Running   0             66m
 topolvm-node-dr26h                    4/4     Running   0             66m
 vg-manager-r6zdv                      1/1     Running   0             66m
 ```

There should be one running instance of `lvms-operator` and `vg-manager`, and multiple instances of `topolvm-controller` and `topolvm-node` depending on the number of nodes.

#### `topolvm-node` is stuck in `Init:0/1`

This error indicates a failure in locating an available disk for LVMS utilization. To investigate further and obtain relevant information, review the status of the `LVMCluster` CR:

```bash
$ oc describe lvmcluster -n openshift-storage
```

If you encounter a failure message such as `no available devices found` while inspecting the status, establish a direct connection to the host where the problem is occurring. From there, run:

```bash
$ lsblk --paths --json -o NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,STATE,KNAME,SERIAL,PARTLABEL,FSTYPE
```

This prints information about the disks on the host. Review this information to see why a device is not considered available for LVMS utilization. For example, if a device has partlabel `bios` or `reserved`, or if they are suspended or read-only, or if they have children disks or `fstype` set, LVMS considers them unavailable. Check [filter.go](../pkg/vgmanager/filter.go) for the complete list of filters LVMS makes use of.

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
