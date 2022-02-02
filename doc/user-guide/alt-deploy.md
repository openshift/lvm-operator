## Deploying via olm

- Clone the repo
```
git clone git@github.com:red-hat-storage/lvm-operator.git && cd lvm-operator
```

### Deploying custom images

- Export required environment variables to be used in next steps
```
export IMAGE_REGISTRY=<quay/docker etc>
export REGISTRY_NAMESPACE=<registry-username>
export IMAGE_TAG=<some-tag>
```
- Build and push the combined operator, vgmanager and metrics image
```
make docker-build-combined docker-push-combined
```
- Build and push the operator bundle image
```
make bundle-build bundle-push
```
- Build and push operator catalog image
```
make catalog-build catalog-push
```
- Finally create LVM operator and follow above instructions for
  creating LVM Cluster CR
```
make deploy-with-olm
```
- This creates:
    - a custom CatalogSource
    - a new openshift-storage Namespace
    - an OperatorGroup
    - a Subscription to the catalog in openshift-storage namespace

- You can check the status of the CSV using the following command:

```
kubectl get csv -n openshift-storage
```
- Once the CSV PHASE is Succeeded you can create a LVMCluster. This can take
  a few minutes

### Installing LVMCluster CR

- After deploying operator, LVMCluster resource can be created using the
  example CR as follows in openshift-storage namespace

```
kubectl create -f config/samples/lvm_v1alpha1_lvmcluster.yaml -n openshift-storage
```

