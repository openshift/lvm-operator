# TopoLVM CSI

- LVM Operator deploys the TopoLVM CSI plugin which provides dynamic provisioning of
  local storage.
- Please refer to TopoLVM [docs][topolvm-docs] for more details on topolvm

## CSI Driver

- *csiDriver* reconcile unit creates the Topolvm CSIDriver resource

## TopoLVM Controller

- *topolvmController* reconcile unit deploys a single TopoLVM Controller plugin
  deployment and manages any updates to the deployment
- The TopoLVM scheduler is not used for pod scheduling. The CSI StorageCapacity
  tracking feature is used by the scheduler to determine the node on which
  to provision storage.
- An init container generates openssl certs to be used in topolvm-controller
  which will be soon replaced with cert-manager

## Topolvm Node and LVMd

- *topolvmNode* reconcile unit deploys and manages the TopoLVM node plugin and lvmd
  daemonset and scales it based on the node selector specified in the devicesClasses
  in LVMCluster
- An init container polls for the availability of lvmd config file before
  starting the lvmd and topolvm-node containers

## Deletion

- All the resources above will be removed by their respective reconcile units when
  LVMCluster CR governing then is deleted


[topolvm-docs]: https://github.com/topolvm/topolvm/tree/main/docs
