# Operator Design

The LVM Operator consists of two managers: 

- The [LVM Operator Manager](lvm-operator-manager.md) runs in a deployment called `lvms-operator` and manages multiple reconciliation units.
- The [Volume Group Manager](vg-manager.md) runs in a daemon set called `vg-manager` and manages a single reconciliation unit.

### Implementation Notes

Each unit of reconciliation should implement the reconcileUnit interface. This will be run by the controller, and errors and success will be propagated to the status and events. This interface is defined in [lvmcluster_controller.go](../../controllers/lvmcluster_controller.go)

```go
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
