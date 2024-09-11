# The LVM Operator Manager

The LVM Operator Manager runs the LVM Cluster controller/reconciler that manages the following reconcile units:

- [LVMCluster Custom Resource (CR)](#lvmcluster-custom-resource-cr)
- [TopoLVM CSI](#topolvm-csi)
  * [CSI Driver](#csi-driver)
  * [TopoLVM Controller](#topolvm-controller)
  * [Topolvm Node and lvmd](#topolvm-node-and-lvmd)
  * [TopoLVM Scheduler](#topolvm-scheduler)
- [Storage Classes](#storage-classes)
- [Volume Group Manager](#volume-group-manager)
- [LVM Volume Groups](#lvm-volume-groups)
- [Openshift Security Context Constraints (SCCs)](#openshift-security-context-constraints-sccs)

Upon receiving a valid [LVMCluster custom resource](#lvmcluster-custom-resource-cr), the LVM Cluster Controller initiates the reconciliation process to set up the TopoLVM Container Storage Interface (CSI) along with all the required resources for using locally available storage through Logical Volume Manager (LVM).

## LVMCluster Custom Resource (CR)

The `LVMCluster` CR is a crucial component of the LVM Operator, as it represents the volume groups that should be created and managed across nodes with custom node selector, toleration, and device selectors. This CR must be created and edited by the user in the namespace where the Operator is also installed. However, it is important to note that only a single CR instance is supported. The user can choose to specify the devices in `deviceSelector.paths` field to be used for the volume group, or if no paths are specified, all available disks will be used. The `status` field is updated based on the status of volume group creation across nodes. It is through the `LVMCluster` CR that the LVM Operator can create and manage the required volume groups, ensuring that they are available for use by the applications running on the OpenShift cluster.

The LVM Cluster Controller generates an LVMVolumeGroup CR for each `deviceClass` present in the LVMCluster CR. The Volume Group Manager controller manages the reconciliation of the LVMVolumeGroups. The LVM Cluster Controller also collates the device class status across nodes from LVMVolumeGroupNodeStatus and updates the status of LVMCluster CR.

> Note: Each device class corresponds to a single volume group.

## TopoLVM CSI

The LVM Operator deploys the TopoLVM CSI plugin, which enables dynamic provisioning of local storage. For more detailed information about TopoLVM, consult the [TopoLVM documentation](https://github.com/topolvm/topolvm/tree/main/docs).

### CSI Driver

The `csiDriver` reconcile unit creates the TopoLVM `CSIDriver` resource.

### TopoLVM Controller

The TopoLVM Controller is embedded into LVM Operator.

### TopoLVM Node and lvmd

The TopoLVM Node and lvmd are embedded into [Volume Group Manager](./vg-manager.md).

### TopoLVM Scheduler

The TopoLVM Scheduler is **not** used in LVMS for scheduling Pods. Instead, the CSI StorageCapacity tracking feature is utilized by the Kubernetes scheduler to determine the Node on which to provision storage. This feature provides the necessary information to the scheduler regarding the available storage on each node, allowing it to make an informed decision about where to place the Pod.

## Storage Classes

The `topolvmStorageClass` reconcile unit is responsible for creating and managing all storage classes associated with the device classes specified in the LVMCluster CR. Each storage class is named with a prefix of `lvms-` followed by the name of the corresponding device class in the LVMCluster CR.

## Volume Group Manager

The `vgManager` reconcile unit is responsible for deploying and managing the [Volume Group Manager](./vg-manager.md).

## LVM Volume Groups

The `lvmVG` reconcile unit is responsible for deploying and managing the LVMVolumeGroup CRs. It creates individual LVMVolumeGroup CRs for each deviceClass specified in the LVMCluster CR. These CRs are then used by the [Volume Group Manager](./vg-manager.md) to create volume groups and generate the lvmd config file for TopoLVM.

## Openshift Security Context Constraints (SCCs)

The Operator requires elevated permissions to interact with the host's LVM commands, which are executed through `nsenter`. When deployed on an OpenShift cluster, all the necessary Security Context Constraints (SCCs) are created by the `openshiftSccs` reconcile unit. This ensures that the `vg-manager` and `topolvm-node` containers have the required permissions to function properly.

## Implementation Notes

Each unit of reconciliation should implement the `Manager` interface. This is run by the controller. Errors and success messages are propagated as Operator status and events. This interface is defined in [manager.go](../../internal/controllers/lvmcluster/resource/manager.go)

```go
type Manager interface {

    // GetName should return a camelCase name of this unit of reconciliation
    GetName() string

    // EnsureCreated should check the resources managed by this unit
    EnsureCreated(Reconciler, context.Context, *lvmv1alpha1.LVMCluster) error

    // EnsureDeleted should wait for the resources to be cleaned up
    EnsureDeleted(Reconciler, context.Context, *lvmv1alpha1.LVMCluster) error
}
```
