# Topolvm CSI

- LVM Operator orchestrates Topolvm CSI by creating and managing all required
  resources for seamless usage of Logical Volumes as Persistent Volumes
- This document outlines only the changes made to deployments of Topolvm CSI
  components and further info please consult topolvm [docs][topolvm-docs]

## CSI Driver

- *csiDriver* reconcile unit deploys Topolvm CSIDriver resource
- Ephemeral volumes support is removed (not in the code) due to below reasons:
  1. There are chances that topolvm project will support Generic Ephemeral
     Volumes and deprecate CSI ephemeral volume support, when that happens
     LVMO will try to include that support
  2. Technical difficulty on deciding default device class when there are
     multiple volume groups across nodes

## Topolvm Controller

- *topolvmController* reconcile unit deploys a single Topolvm Controller plugin
  deployment and manages any updates to the deployment
- Topolvm scheduler is not used for pod scheduling but only CSI StorageCapacity
  tracking feature is used
- An init container generates openssl certs to be used in topolvm controller
  which will be soon replaced with cert-manager

## Topolvm Node and LVMd

- *topolvmNode* reconcile unit deploys and manages topolvm node plugin and lvmd
  daemonset and scales it based on node selector on which volume groups exists
- An init container polls for the availability of lvmd config file before start
  of lvmd and topolvm-node containers

## Deletion

- All above resources will be removed by their respective reconcile units when
  LVM Cluster CR governing then is deleted


[topolvm-docs]: https://github.com/topolvm/topolvm/tree/main/docs
