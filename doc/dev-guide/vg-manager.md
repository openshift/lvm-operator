# Volume Group Manager

## Creation

- `vg-manager` daemonset pods are created by the LVMCluster controller on LVMCluster CR creation 
- They run on all nodes which match the Node Selector specified in
  the CR. They run on all schedulable nodes if no nodeSelector is specified.
- A controller owner reference is set on the daemonset so it is cleaned up
  when the LVMCluster CR is deleted.

## Reconciliation

- The vg-manager daemonset consists of individual controller pods, each of
  which handles the on node operations for the node it is running on.
- The vg-manager controller reconciles LVMVolumeGroup CRs which are created
  by the LVMO.
- The vg-manager will determine the disks that match the filters
  specified (currently not implemented) on the node it is running on and create
  an LVM VG with them. It then creates the lvmd.yaml config file for lvmd. 
- vg-manager also updates LVMVolumeGroupStatus with observed status of volume
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
     limits topolvm nodeplugin not to be deployed as a daemonset since configmap
     should be unique for a daemonset
