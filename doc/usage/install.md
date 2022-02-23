# Using the LVM-Operator

Because there currently is no CI pipeline that builds this repo, you will either have to build it yourself, or use a prebuilt state made available. The prebuilt state might be out of sync with what is currently in the repo

## Preparations

### Building the Operator yourself

Building the operator is easy. Just make sure you have docker or podman installed on your system and that you are logged into your registry (quay.io, docker, ...)

Set the environment variable `IMG` to the new repository path where you want to host your image:

```
export IMG=quay.io/USER/lvm-operator
```

Then start the build process like this:

```
make docker-build-combined docker-push
```

Ensure that the new repository in your registry is either set to public or that your target OpenShift cluster has read access to that repository.

When this is finished, you are ready to continue with the deploy steps

### Using the pre-built image

If you are ok with using the prebuilt images, then just set your variable like this:

```
export IMG=quay.io/ocs-dev/lvm-operator
```

## Deploy

Ensured that your `IMG` variable is set to a repository that contains the operator.
Afterwards ensure that you are connected to the right cluster:

```
oc get nodes
```

After you have ensured both, you can start the deployment with

```
make deploy
```

After this has finished successfully, you should switch over to the lvm-operator namespace:

`oc project lvm-operator-system`

Please wait until all Pods are finished running:

```
oc get pods -w
```

After the controller is running, create the sample lvmCluster CR:

```
oc create -n lvm-operator-system -f https://github.com/red-hat-storage/lvm-operator/raw/main/config/samples/lvm_v1alpha1_lvmcluster.yaml
```

This will try to leverage all nodes and all available disks on these nodes.

Again please wait until all Pods are finished running:

```
oc get pods -w
```

The `topolvm-node` pod will be in init until `vg-manager` has done all the preparation and this might take a while.

After all Pods are running, you will get a storage class that you can use when creating a PVCs:

```
oc get sc

NAME          PROVISIONER          RECLAIMPOLICY   VOLUMEBINDINGMODE      ALLOWVOLUMEEXPANSION   AGE
odf-lvm-vg1   topolvm.cybozu.com   Delete          WaitForFirstConsumer   true                   3m44s
```

## Test

After the operator is installed and the Cluster is set up, you can start by creating a simple test application that will consume storage:

Create the PVC:

```yaml
cat <<EOF | oc apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: lvmpvc
  labels:
    type: local
spec:
  storageClassName: odf-lvm-vg1
  resources:
    requests:
      storage: 5Gi
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
EOF
```

You will see that the PVC will be stuck in the pending state:

```
oc get pvc

NAME     STATUS    VOLUME   CAPACITY   ACCESS MODES   STORAGECLASS   AGE
lvmpvc   Pending                                      odf-lvm-vg1    7s
```

That's because our Storage Class is waiting for a Pod that needs that PVC, before it is created. So let's create a Pod for this PVC.

```yaml
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: lvmpod
spec:
  volumes:
    - name: storage
      persistentVolumeClaim:
        claimName: lvmpvc
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

After we added the Pod, the PVC will be bound and the Pod will eventually be Running:

```
oc get pvc,pods

NAME                           STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
persistentvolumeclaim/lvmpvc   Bound    pvc-a37ef71c-a9b9-45d8-96e8-3b5ad30a84f6   5Gi        RWO            odf-lvm-vg1    3m2s

NAME         READY   STATUS    RESTARTS   AGE
pod/lvmpod   1/1     Running   0          28s
```
