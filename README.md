# LVM Operator

LVM Operator is a local storage orchestrator for Kubernetes which manages
[Topolvm][topolvm_repo]. It creates Logical Volume Groups, deploys Topolvm
CSI and manages them.

## Installation

### Deploying pre-built images

The lvm operator can be installed into a kubernetes cluster using Operator
Lifecycle Manager (OLM).

For quick install using pre-built container images.

```
make deploy-with-olm
```

This creates:
- a custom CatalogSource
- a new openshift-storage Namespace
- an OperatorGroup
- a Subscription to the catalog in openshift-storage namespace

You can check the status of the CSV using the following command:

```
kubectl get csv -n openshift-storage
```

This can take a few minutes. Once PHASE state is Succeeded you can create
a LVMCluster.

From the CLI, as LVMCluster resource can be created using the example CR
as follows in openshift-storage namespace

```
kubectl create -f config/samples/lvm_v1alpha1_lvmcluster.yaml -n openshift-storage
```

### Deploying custom images

- Clone the repo
```
git clone git@github.com:red-hat-storage/lvm-operator.git && cd lvm-operator
```

#### via OLM
- Export required environment variables to be used in next steps
```
export IMAGE_REGISTRY=<quay/docker etc>
export REGISTRY_NAMESPACE=<registry-username>
export IMAGE_TAG=<some-tag>
export OPERATOR_NAMESPACE=<namespace>
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
- Finally create LVM operator and follow [above][pre-built] instructions for
  creating LVM Cluster CR
```
make deploy-with-olm
```

#### via manifests
- Export required environment variables to be used in next steps, either set
  a single variable `IMG` or different vars as set as above
```
IMG=<IMAGE_REGISTRY>/<REGISTRY_NAMESPACE>/<IMAGE_NAME>:<IMAGE_TAG> make docker-build-combined docker-push-combined deploy
```
- Create LVM Cluster CR by which required resources for managing topolvm will
  be deployed
```
kubectl create -f config/samples/lvm_v1alpha1_lvmcluster.yaml -n lvm-operator-system
```
- Note: Resources are deployed in `lvm-operator-system` namespace when deployed
  via manifests

## Documentation

- Please refer [docs][docs] directory for user and development guides.

## Contribution

To contribute to the project follow the [contribution][contribution] guide.

[topolvm_repo]: https://github.com/topolvm/topolvm
[pre-built]: #deploying-pre-built-images
[docs]: ./docs/README.md
[contribution]: ./CONTRIBUTING.md
