# The Volume Group Manager

The Volume Group Manager manages a single controller/reconciler, which runs as `vg-manager` daemon set pods on a cluster. They are responsible for performing on-node operations for the node they are running on. They first identify disks that match the filters specified for the node. Next, they watch for the LVMVolumeGroup resource and create the necessary volume groups and thin pools on the node based on the specified deviceSelector and nodeSelector. Once the volume groups are created, vg-manager generates the `lvmd.yaml` configuration file for lvmd to use. Additionally, vg-manager updates the LVMVolumeGroupStatus with the observed status of the volume groups on the node where it is running.

## Deletion

A controller owner reference is set on the daemon set, so it is cleaned up when the LVMCluster CR is deleted.

## Considerations

Storing the lvmd config file on the host provides a superior solution when compared to other options:
- Single config map: The process of storing the configuration file contents of lvmd across multiple nodes in a single config map requires additional processing to segment the configuration based on each individual node and store it accordingly before it can be consumed by lvmd.
- Multiple config maps: Although technically possible, using multiple config maps to store lvmd config file contents across nodes would limit the deployment of TopoLVM node plugin as a daemon set. This is because each daemon set requires a unique config map.
