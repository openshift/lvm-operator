## LVM Volume Groups

- *lvmVG* reconcile units deploys and manages LVMVolumeGroup CRs
- Based on LVMCluster CR individual volume groups with node and device selector
  is filtered and corresponding CRs are deployed
- The corresponding CRs forms the basis of `vgManager` unit to create volume
  groups and create lvmd config file

## Openshift SCCs

- When the operator is deployed on an openshift environment all the required
  SCCs are set up by `openshiftSccs` reconcile unit
- By virtue of the logical volumes usage, `vg-manager`, `topolvm-node` and
  `lvmd` containers needs elevated permissions to access host LVM commands
- Required operations with elevated permissions will be run using `nsenter`
  into some of the host namespaces

## Storage Classes

- *topolvmStorageClass* resource units creates and manages all the storage
  class corresponding to the volume groups existing across the cluster
- Storage Class name is generated with a prefix add to name of device class
  supplied while creating LVMCluster CR
