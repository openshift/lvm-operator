# Topolvm CSI

- LVM Operator deploys the Topolvm CSI which provides dynamic provisioning of
  local storage.
- Please refer to topolvm [docs][topolvm-docs] for more details on topolvm

## CSI Driver

- *csiDriver* reconcile unit deploys Topolvm CSIDriver resource

## Topolvm Controller

- *topolvmController* reconcile unit deploys a single Topolvm Controller plugin
  deployment and manages any updates to the deployment
- Topolvm scheduler is not used for pod scheduling. The CSI StorageCapacity
  tracking feature by the scheduler
- An init container generates openssl certs to be used in topolvm controller
  which will be soon replaced with cert-manager

## Topolvm Node and LVMd

- *topolvmNode* reconcile unit deploys and manages topolvm node plugin and lvmd
  daemonset and scales it based on node selector specified in the devicesClasses
  in LVMCluster
- An init container polls for the availability of lvmd config file before
  starting the lvmd and topolvm-node containers

## Deletion

- All above resources will be removed by their respective reconcile units when
  LVM Cluster CR governing then is deleted


[topolvm-docs]: https://github.com/topolvm/topolvm/tree/main/docs
