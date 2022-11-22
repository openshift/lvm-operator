## Deploying LVMCluster CR

This guide assumes you followed steps in [Readme][repo-readme] and the LVM operator
(hereafter LVMO) is running in your cluster.

The available disks in our test kubernetes cluster node are listed below. There are
no existing LVM Physical Volumes, Volume Groups and Logical Volumes

``` console
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

In our setup, the LVMO is installed in `lvm-operator-system` namespace via the `make deploy`
target. The same operations hold good if LVMO is installed in any other namespaces.

``` console
kubectl get pods
NAME                                              READY   STATUS    RESTARTS   AGE
lvm-operator-controller-manager-8bf864c85-8zjlp   3/3     Running   0          96s
```

After all containers in the pod are in `READY` state we can proceed with
deploying LVMCluster CR

``` yaml
cat <<EOF | kubectl apply -f -
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: lvmcluster-sample
  # set the namespace to the operator namespace
  namespace: lvm-operator-system
spec:
  storage:
    deviceClasses:
    - name: vg1
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10    
EOF
```

- After deploying the CR :
  - an LVM volume group named vg1 will be created using all free disks on the cluster.
  - a thinpool called thin-pool-1 will be created in vg1 with size equal to 90% of the vg.
  - the topolvm csi plugin is deployed. The topolvm-controller-* and topolvm-node-* pods will start up.
  - a storageclass and a volumesnapshotclass, both named odf-lvm-vg1, are created


- Wait for all the pods to be in `READY` state
- The `topolvm-controller` and `topolvm-node` pods will remain in `init` stage for a while
  awaiting creation of certs and creation of volume groups by `vg-manager`
  respectively

``` console
kubectl get pods
NAME                                  READY   STATUS    RESTARTS   AGE
lvm-operator-controller-manager-8bf864c85-8zjlp    3/3     Running   0          31m
topolvm-controller-694bc98b87-x6dxn   4/4     Running   0          10m
topolvm-node-pbcth                    4/4     Running   0          10m
vg-manager-f979f                      1/1     Running   0          10m
```
When all pods are ready, the LVM physical volumes and volume groups will have been
created on the host.

``` console
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

Confirm that the following resources have been created on the cluster:

- When the LVMCluster CR is deployed, two supporting, internal Custom Resources
  are created by LVMO
- LVMVolumeGroup: CR created and managed by LVMO which tracks individual volume
  groups across multiple nodes
``` yaml
# kubectl get lvmvolumegroup vg1 -oyaml
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMVolumeGroup
metadata:
  creationTimestamp: "2022-02-02T05:16:42Z"
  generation: 1
  name: vg1
  namespace: lvm-operator-system
  resourceVersion: "17242461"
  uid: 88e8ad7d-1544-41fb-9a8e-12b1a66ab157
spec: {}
```
- LVMVolumeGroupNodeStatus: CR created and managed by VG Manager which tracks
  the status of the volumegroups on a node
``` yaml
# kubectl get lvmvolumegroupnodestatuses.lvm.topolvm.io kube-node -oyaml
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMVolumeGroupNodeStatus
metadata:
  creationTimestamp: "2022-02-02T05:17:59Z"
  generation: 1
  name: kube-node
  namespace: lvm-operator-system
  resourceVersion: "17242882"
  uid: 292de9bb-3a9b-4ee8-946a-9b587986dafd
spec:
  nodeStatus:
    - devices:
        - /dev/nvme0n1
        - /dev/nvme1n1
        - /dev/nvme2n1
      name: vg1
      status: Ready
```
- A StorageClass called `odf-lvm-<deviceclassname>` will be created
``` console
# kubectl get storageclass
NAME          PROVISIONER          RECLAIMPOLICY   VOLUMEBINDINGMODE      ALLOWVOLUMEEXPANSION   AGE
odf-lvm-vg1   topolvm.io           Delete          WaitForFirstConsumer   true                   31m
```
```
- A Volumesnapshotclass called `odf-lvm-<deviceclassname>` will be created
``` console
# kubectl get volumesnapshotclass
NAME          DRIVER               DELETIONPOLICY   AGE
odf-lvm-vg1   topolvm.io           Delete           24h

```
Note:
- Reconciling multiple LVMCluster CRs by LVMO is not supported
- Custom resources LVMVolumeGroup and LVMVolumeGroupNodeStatus are managed by
  LVMO and users should not edit them.
- At present, there can only be one deviceClass entry in the LVMCluster CR.



- The status of the LVMCluster is available in the status field.
```json
# kubectl get lvmclusters.lvm.topolvm.io -ojsonpath='{.items[*].status.deviceClassStatuses[*]}' | python3 -mjson.tool
{
    "name": "vg1",
    "nodeStatus": [
        {
            "devices": [
                "/dev/nvme0n1",
                "/dev/nvme1n1",
                "/dev/nvme2n1"
            ],
            "node": "kube-node",
            "status": "Ready"
        }
    ]
}
```
## Deploying PVC and App Pod

- Create a PVC using the StorageClass. The PVC will not be bound until a pod 
claims the storage as the volume binding mode is set to WaitForFirstConsumer.

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: lvm-file-pvc
spec:
  volumeMode: Filesystem
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
  storageClassName: odf-lvm-vg1
EOF
```
``` console
kubectl get pvc
NAME           STATUS    VOLUME   CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvm-file-pvc   Pending                                      odf-lvm-vg1    12s
```
- In a multi node setup the `WaitForFirstConsumer` volume binding is used to
  take scheduling decisions of pod placement which usually prefers the node
  having highest amount of available storage space
- Deploy an app pod that uses the PVC created earlier.
``` yaml
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: app-file
spec:
  containers:
  - name: app-file
    image: bash
    imagePullPolicy: IfNotPresent
    command: ["/usr/local/bin/bash", "-c", "/usr/bin/tail -f /dev/null"]
    volumeMounts:
    - mountPath: "/mnt/file"
      name: lvm-file-pvc
  volumes:
    - name: lvm-file-pvc
      persistentVolumeClaim:
        claimName: lvm-file-pvc
EOF
```
- Once the pod is scheduled, the PVC is provisioned and bound.
``` console
kubectl get pod | grep -E '^(app|NAME)'
NAME                                  READY   STATUS    RESTARTS   AGE
app-file                              1/1     Running   0          2m6s

kubectl get pvc
NAME           STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvm-file-pvc   Bound    pvc-04a4f5f7-8665-4008-818a-490503c859f5   5Gi        RWO            odf-lvm-vg1    5m55s
```
- To create a PVC with `volumeMode: Block`, do the following:
``` yaml
cat <<EOF | kubectl apply -f -
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: lvm-block-pvc
spec:
  volumeMode: Block
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
  storageClassName: odf-lvm-vg1
---
apiVersion: v1
kind: Pod
metadata:
  name: app-block
spec:
  containers:
  - name: app-block
    image: bash
    imagePullPolicy: IfNotPresent
    command: ["/usr/local/bin/bash", "-c", "/usr/bin/tail -f /dev/null"]
    volumeDevices:
    - devicePath: "/dev/xvda"
      name: lvm-block-pvc
  volumes:
    - name: lvm-block-pvc
      persistentVolumeClaim:
        claimName: lvm-block-pvc
EOF
```
- Check the status of both PVCs and App pods:
``` console
kubectl get pod | grep -E '^(app|NAME)'
NAME                                  READY   STATUS    RESTARTS   AGE
app-block                             1/1     Running   0          99s
app-file                              1/1     Running   0          9m34s

kubectl get pvc
NAME            STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvm-block-pvc   Bound    pvc-56fe959a-1184-4fa6-9d66-b69c06d43cf4   5Gi        RWO            odf-lvm-vg1    104s
lvm-file-pvc    Bound    pvc-04a4f5f7-8665-4008-818a-490503c859f5   5Gi        RWO            odf-lvm-vg1    13m
```

### Monitoring and Metrics

- LVMO currently surfaces only Topolvm metrics and those can be viewed either
  from UI or port-forwarding the service
- On Openshift clusters, set the label "openshift.io/cluster-monitoring" on the
  namespace the LVMO is running in.
``` console
kubectl patch namespace/lvm-operator-system -p '{"metadata": {"labels": {"openshift.io/cluster-monitoring": "true"}}}'
```
``` console
# port-forward service in one terminal
kubectl port-forward svc/topolvm-node-metrics :8080
Forwarding from 127.0.0.1:41685 -> 8080
Forwarding from [::1]:41685 -> 8080
...
...

# in another terminal view the metrics at above port in localhost
curl -s 127.0.0.1:41685/metrics | grep -Ei 'topolvm_volumegroup_.*?_bytes\{'
topolvm_volumegroup_available_bytes{device_class="vg1",node="kube-node"} 4.790222323712e+12
topolvm_volumegroup_size_bytes{device_class="vg1",node="kube-node"} 4.800959741952e+12
```

## Cleanup

### Steps

1. Remove all the apps which are using PVCs created with topolvm
``` console
# delete App pod first
kubectl delete pod app-file app-block
pod "app-file" deleted
pod "app-block" deleted

# delete PVCs which were used by above App pods
kubectl delete pvc lvm-file-pvc lvm-block-pvc
persistentvolumeclaim "lvm-file-pvc" deleted
persistentvolumeclaim "lvm-block-pvc" deleted
```
2. Make sure there are no `logicalvolumes` CRs which were created by topolvm
``` console
kubectl get logicalvolumes.topolvm.io 
No resources found
```
3. Take a json dump of LVMCluster CR contents. It has the list of VGs and PVs
  created on the nodes.
``` console
kubectl get lvmclusters.lvm.topolvm.io -ojson > /tmp/lvmcr.json
```
4. Either parse contents from above json file via jq or store status of the CR
``` console
kubectl get lvmclusters.lvm.topolvm.io -ojsonpath='{.items[*].status.deviceClassStatuses[*]}' | python3 -mjson.tool
```
``` json
{
    "name": "vg1",
    "nodeStatus": [
        {
            "devices": [
                "/dev/nvme0n1",
                "/dev/nvme1n1",
                "/dev/nvme2n1"
            ],
            "node": "kube-node",
            "status": "Ready"
        }
    ]
}
```
5. Remove LVMCluster CR and wait for deletion of all resources in the namespace
   except the operator (controller-manager pod)
``` console
kubectl delete lvmclusters.lvm.topolvm.io lvmcluster-sample
lvmcluster.lvm.topolvm.io "lvmcluster-sample" deleted

kubectl get po
NAME                                 READY   STATUS    RESTARTS   AGE
lvm-operator-controller-manager-8bf864c85-8zjlp   3/3     Running   0          125m
```

## Uninstalling LVMO

- LVMO can be removed from the cluster via one of the following methods based
  on your installation
``` console
# if deployed via manifests
make undeploy

# if deployed via olm
make undeploy-with-olm
```

[repo-readme]: ../../README.md
