## Deploying LVMCluster CR

This guide assumes you followed steps in [Readme][repo-readme] and LVM operator
(hereafter LVMO) is running in your cluster.

Below are the available disks in one of the kubernetes nodes and there are no
existing Physical Volumes, Volume Groups and Logical Volumes

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

Here LVMO is installed in `lvm-operator-system` namespace via `make deploy`
target and operations will not change if LVMO is installed in any other namespaces.

``` console
kubectl get pods
NAME                                 READY   STATUS    RESTARTS   AGE
controller-manager-8bf864c85-8zjlp   3/3     Running   0          96s
```

After all containers in above listing is in `READY` state we can proceed with
deploying LVMCluster CR

``` yaml
cat <<EOF | kubectl apply -f -
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: lvmcluster-sample
  # replace the namespace to suit your installation
  namespace: lvm-operator-system
spec:
  deviceClasses:
  - name: vg1
EOF
```

- Ability to respect Node Selector, Tolerations and Device Selectors in above CR
  is coming soon.
- After deploying above CR all free disks will be used to create a volume
  group, topolvm csi is deployed and all the corresponding resources are
  created, including storage class for ready consumption of Persistent Volumes
  based on Topolvm

- Wait for all the pods to be in `READY` state
- `topolvm-controller` and `topolvm-node` will be in `init` stage for a while
  awaiting creation of certs and creation of volume groups by `vg-manager`
  respectively

``` console
kubectl get pods
NAME                                  READY   STATUS    RESTARTS   AGE
controller-manager-8bf864c85-8zjlp    3/3     Running   0          31m
topolvm-controller-694bc98b87-x6dxn   4/4     Running   0          10m
topolvm-node-pbcth                    4/4     Running   0          10m
vg-manager-f979f                      1/1     Running   0          10m
```
Most notable changes by end of above operation is creation of physical volumes
and volume groups

``` console
sh-4.4# pvs
  PV           VG  Fmt  Attr PSize  PFree 
  /dev/nvme0n1 vg1 lvm2 a--  <1.46t <1.46t
  /dev/nvme1n1 vg1 lvm2 a--  <1.46t <1.46t
  /dev/nvme2n1 vg1 lvm2 a--  <1.46t <1.46t
sh-4.4# vgs
  VG  #PV #LV #SN Attr   VSize  VFree 
  vg1   3   0   0 wz--n- <4.37t <4.37t
```

Before moving further we can look for other resources apart from above pods
created by LVMO after deploying LVMCluster CR

- When LVMCluster CR is deployed, which is expected to be a single entity
  containing details of all volume groups that needs to be created across
  multiple nodes, two supporting Custom Resources are created by LVMO
- LVMVolumeGroup: CR created and managed by LVMO which tracks individual volume
  groups across multiple nodes
- LVMVolumeGroupNodeStatus: CR created and managed by LVMO which tracks node status
  corresponding to multiple volume groups
- A storage class which matches names (prefixed with `topolvm-`) of
  deviceClasses in LVMCluster CR are created as below
``` console
# kubectl get storageclass
NAME          PROVISIONER          RECLAIMPOLICY   VOLUMEBINDINGMODE      ALLOWVOLUMEEXPANSION   AGE
topolvm-vg1   topolvm.cybozu.com   Delete          WaitForFirstConsumer   true                   31m
```

Note:
- Reconciling multiple LVMCluster CRs by LVMO is not supported and erroneous
- Custom resources LVMVolumeGroup and LVMVolumeGroupNodeStatus are managed by
  LVMO and if need may arise users are expected to **ONLY** edit LVMCluster CR

## Deploying PVC and App Pod

- A successful reconciliation of LVMCluster CR will setup all the underlying
  resources needed to be able to create a PVC and use it in app pod
- Create a PVC as below and as the storage class created by LVMO is of
  `WaitForFirstConsumer` volume binding mode, the binding is delay until a pod
  claims the storage
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
  storageClassName: topolvm-vg1
EOF
```
``` console
kubectl get pvc
NAME           STATUS    VOLUME   CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvm-file-pvc   Pending                                      topolvm-vg1    12s
```
- In a multi node setup this volume binding is used to take scheduling
  decisions of pod placement which usually prefers the node having highest
  amount of available storage space
- Deploy an app pod claiming above created PVC
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
- App pod should claim the PVC and be in `READY`, PVC will get bound to this
  app
``` console
kubectl get pod | grep -E '^(app|NAME)'
NAME                                  READY   STATUS    RESTARTS   AGE
app-file                              1/1     Running   0          2m6s

kubectl get pvc
NAME           STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvm-file-pvc   Bound    pvc-04a4f5f7-8665-4008-818a-490503c859f5   5Gi        RWO            topolvm-vg1    5m55s
```
- In a similar way, PVC with `volumeMode: Block` can also be created and
  claimed by App pod as below
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
  storageClassName: topolvm-vg1
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
- Status of both PVCs and App pods can be referred as below
``` console
kubectl get pod | grep -E '^(app|NAME)'
NAME                                  READY   STATUS    RESTARTS   AGE
app-block                             1/1     Running   0          99s
app-file                              1/1     Running   0          9m34s

kubectl get pvc
NAME            STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvm-block-pvc   Bound    pvc-56fe959a-1184-4fa6-9d66-b69c06d43cf4   5Gi        RWO            topolvm-vg1    104s
lvm-file-pvc    Bound    pvc-04a4f5f7-8665-4008-818a-490503c859f5   5Gi        RWO            topolvm-vg1    13m
```

### Monitoring and Metrics

- LVMO currently surfaces only Topolvm metrics and those can be viewed either
  from UI or port-forwarding the service
- Make sure the namespace LVMO is running in has below label or patch the
  namespace for metrics to be available in UI
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

- Feature to auto cleanup volume groups after removing LVMCluster CR is coming
  soon
- Until then, please follow below procedure to cleanup left out resources
  during CR deletion

### Steps

1. Make sure no PVC is bound to an App pod and remove the PVCs, all app pods can be removed in parallel and corresponding PVCs can be removed after pod deletion, below deletion is only for representation
``` console
# delete App pod first
kubectl delete pod app-file
pod "app-file" deleted

# delete PVC which was used by above App pod
kubectl delete pvc lvm-file-pvc
persistentvolumeclaim "lvm-file-pvc" deleted

# likewise remove for rest of App pods and corresponding PVCs
kubectl delete pod app-block
pod "app-block" deleted

kubectl delete pvc lvm-block-pvc
persistentvolumeclaim "lvm-block-pvc" deleted
```
2. Make sure there are no `logicalvolumes` CRs which were created by topolvm
``` console
kubectl get logicalvolumes.topolvm.cybozu.com 
No resources found
```
3. Take a json dump of LVMCluster CR contents which helps in finding out Volume
   Groups and Physical Volumes that are created by LVMO
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
   except operator (contorller-manager pod)
``` console
kubectl delete lvmclusters.lvm.topolvm.io lvmcluster-sample
lvmcluster.lvm.topolvm.io "lvmcluster-sample" deleted

kubectl get po
NAME                                 READY   STATUS    RESTARTS   AGE
controller-manager-8bf864c85-8zjlp   3/3     Running   0          125m
```
6. Login to the node and remove above noted volume groups and physical volumes
``` console
sh-4.4# vgremove vg1 --nolock
  WARNING: File locking is disabled.
  Volume group "vg1" successfully removed
sh-4.4# pvremove  /dev/nvme0n1 /dev/nvme1n1 /dev/nvme2n1 --nolock
  WARNING: File locking is disabled.
  Labels on physical volume "/dev/nvme0n1" successfully wiped.
  Labels on physical volume "/dev/nvme1n1" successfully wiped.
  Labels on physical volume "/dev/nvme2n1" successfully wiped.
```
7. `vg-manager` pod creates lvmd config file to be used in `topolvm-node` pod
   during initialization and please remove that file as well from node
``` console
sh-4.4# rm /etc/topolvm/lvmd.yaml 
```
Note:
- Removal of volume groups, physical volumes and lvm config file is necessary
  during cleanup or else LVMO fails to deploy Topolvm in next iteration.

## Undeploying LVMO

- LVMO can be removed from the cluster via one of the following methods based
  on your installation
``` console
# if deployed via manifests
make undeploy

# if deployed via olm
make undeploy-with-olm
```

[repo-readme]: ../../README.md
