# LVM Operator

The LVM Operator provides the ability to dynamically provision local storage
via [Topolvm][topolvm_repo] CSI plugin. It creates LVM volume groups and
manages them.

## Installation

## Building and deploying LVM Operator

- Clone the repo
```
git clone git@github.com:red-hat-storage/lvm-operator.git && cd lvm-operator
```
- Export required environment variables to be used in next steps, either set
  a single variable `IMG` or individual vars can be set as follows
```
IMG=<IMAGE_REGISTRY>/<REGISTRY_NAMESPACE>/<IMAGE_NAME>:<IMAGE_TAG> make docker-build-combined docker-push-combined deploy
```
- Create the LVMCluster CR which will cause the operator to create the
  volumegroup and deploy topolvm
```
kubectl create -f config/samples/lvm_v1alpha1_lvmcluster.yaml -n lvm-operator-system
```
- Note: Resources are deployed in `lvm-operator-system` namespace when deployed
  via manifests

## Documentation

- Please refer to the [docs][doc] directory for user and development guides.

## Contribution

To contribute to the project follow the [contribution][contribution] guide.

[topolvm_repo]: https://github.com/topolvm/topolvm
[doc]: ./doc/index.md
[contribution]: ./CONTRIBUTING.md

