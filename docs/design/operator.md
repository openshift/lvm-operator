# Operator design

# Controllers and their managed resources


- **lvmcluster-controller:** Running in the operator deployment, it will create all resources that don't require information from the node. When applicable, the health of the underlying resource is updated in the LVMCluster status.:
    - vgmanager daemonset
    - lvmd daemonset
    - TopoLVM CSIDriver CR
    - TopoLVM CSI Driver Controller Deployment (controller is the name of the csi-component)
    - TopoLVM CSI Driver Node Daemonset
      - needs an initContainer to block until lvmd config file is read
- **The vg-manager:** A daemonset with one instance per selected node, it will create all resources that require knowledge from the node.
    - volumegroups and thinpools
    - lvmd config file



Each unit of reconciliation should implement the reconcileUnit interface.
This will be run by the controller, and errors and success will be propagated to the status and events.
This interface is defined in [lvmcluster_controller.go](../../controllers/lvmcluster_controller.go)

```
type resourceManager interface {

	// getName should return a camelCase name of this unit of reconciliation
	getName() string

	// ensureCreated should check the resources managed by this unit
	ensureCreated(*LVMClusterReconciler, context.Context, lvmv1alpha1.LVMCluster) error

	// ensureDeleted should wait for the resources to be cleaned up
	ensureDeleted(*LVMClusterReconciler, context.Context, lvmv1alpha1.LVMCluster) error

	// updateStatus should optionally update the CR's status about the health of the managed resource
	// each unit will have updateStatus called induvidually so
	// avoid status fields like lastHeartbeatTime and have a
	// status that changes only when the operands change.
	updateStatus(*LVMClusterReconciler, context.Context, lvmv1alpha1.LVMCluster) error
}
```
