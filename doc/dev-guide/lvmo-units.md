## LVM Volume Groups

- *lvmVG* reconcile units deploys and manages LVMVolumeGroup CRs
- The LVMVG resource manager creates individual LVMVolumeGroup CRs for each
  deviceClass in the LVMCluster CR. The vgmanager controller watches the LVMVolumeGroup
  and creates the required volume groups on the individual nodes based on the
  specified deviceSelector and nodeSelector.
- The corresponding CRs forms the basis of `vgManager` unit to create volume
  groups and the lvmd config file for TopoLVM.

## Openshift SCCs

- When the operator is deployed on an Openshift cluster all the required
  SCCs are created by `openshiftSccs` reconcile unit
- The `vg-manager`, `topolvm-node` and `lvmd` containers need elevated
  permissions to access host LVM commands using `nsenter`.

## Storage Classes

- *topolvmStorageClass* resource units creates and manages all the storage
  classes corresponding to the deviceClasses in the LVMCluster
- Storage Class name is generated with a prefix "lvms-" added to name of the
  device class in the LVMCluster CR
