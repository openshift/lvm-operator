# Operator design

## Controllers and their managed resources

### lvmcluster-controller

- On receiving a valid LVMCluster CR, lvmcluster-controller reconciles below
  resource units for setting up [Topolvm](topolvm-repo) CSI and all the
  supporting resources to use storage local to the node via Logical Volume
  Manager (lvm)
- *csiDriver*: Reconciles topolvm CSI Driver
- *topolvmController*: Reconciles topolvm controller plugin
- *lvmVG*: Reconciles volume groups from LVMCluster CR
- *openshiftSccs*: Manages SCCs when the operator is run in Openshift
  environment
- *topolvmNode*: Reconciles topolvm nodeplugin along with lvmd
- *vgManager*: Responsible for creation of Volume Groups
- *topolvmStorageClass*: Manages storage class life cycle based on
  devicesClasses in LVMCluster CR
- In addition to managing above resource units, lvmcluster-controller collates
  the status of deviceClasses across nodes from LVMVolumeGroupNodeStatus and
  updates status of LVMCluster CR

Note:
- Above names refers to the struct which satisfies `resourceManager` interface
  (mentioned towards the end)
- Please refer topolvm [design][topolvm-design] doc to know more about Topolvm
  CSI
- Any new resource units should also satisfy `resourceManager` interface


- `resourceManager` interface is defined in [lvmcluster_controller.go](../../controllers/lvmcluster_controller.go) and as per the event received to the lvmcluster-controller concerned methods on the resource units are invoked
- Depending on the resource unit some of the methods can be no-op

``` go
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

[topolvm-repo]: https://github.com/topolvm/topolvm
[topolvm-design]: https://github.com/topolvm/topolvm/blob/main/docs/design.md
