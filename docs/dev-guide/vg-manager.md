# Volume Group Manager

## Creation

- On LVMCluster CR creation `vg-manager` daemonset pods are created
- They'll be run on all the nodes which matches the Node Selector specified in
  the CR, as of now it's run on all schedulable nodes
- A controller owner reference is set on the daemonset to be able to cleanup
  itself when CR is deleted

## Reconciliation

- On detecting the presence of LVMVolumeGroup CR which are created by LVMO
  based on LVMCluster CR, controller part of `vg-manager` will start discovery
  of disks and filters usable disks for creation of volume groups
- On filtered disks, physical volumes and volume groups are created
- This operation is individual and unique to the node where the controller part
  of `vg-manager` pod is running
- As a final step, it writes LVMd config file via hostPath with volume group
  and lvmd socket contents for topolvm csi to make use of underlying volume
  groups
- `lvmd` and topolvm nodeplugin will have access to this config file via
  hostPath to perform their own functionalities
- `vg-manager` also updates LVMVolumeGroupStatus with observed status of volume
  groups for the node on which it is running


## Deletion

- `vg-manager` daemonset is garbage collected when LVMCluster CR is deleted

## Considerations

- Storing lvmd config file on host seemed to be better when compared against
  below options:
  1. Single configmap: Storing all the lvmd config file contents across nodes
     into a single configmap involves extra processing to segment the config
     according to the node and save that before being consumed by lvmd
  2. Multiple configmaps: Although this is doable having multiple configmaps
     limts topolvm nodeplugin not to be deployed as a daemonset since configmap
     should be unique for a daemonset
