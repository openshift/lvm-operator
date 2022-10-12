# Operator design

## Controllers and their managed resources

### lvmcluster-controller

- On receiving a valid LVMCluster CR, lvmcluster-controller reconciles the
  following resource units for setting up [Topolvm](topolvm-repo) CSI and all
  the supporting resources to use storage local to the node via Logical Volume
  Manager (lvm)
- *csiDriver*: Reconciles TopoLVM CSI Driver
- *topolvmController*: Reconciles TopoLVM controller plugin
- *lvmVG*: Reconciles volume groups from LVMCluster CR
- *openshiftSccs*: Manages SCCs when the operator is run in Openshift
  environment
- *topolvmNode*: Reconciles TopoLVM nodeplugin along with lvmd
- *vgManager*: Responsible for creation of Volume Groups
- *topolvmStorageClass*: Manages storage class life cycle based on
  devicesClasses in LVMCluster CR
- The LVMO creates an LVMVolumeGroup CR for each deviceClass in the
  LVMCluster CR. The LVMVolumeGroups are reconciled by the vgmanager controllers.
- In addition to managing the above resource units, lvmcluster-controller collates
  the status of deviceClasses across nodes from LVMVolumeGroupNodeStatus and
  updates status of LVMCluster CR
- `resourceManager` interface is defined in
  [lvmcluster_controller.go][contorller]
- Depending on the resource unit some of the methods can be no-op

Note:
- Above names refers to the struct which satisfies `resourceManager` interface
- Please refer to the topolvm [design][topolvm-design] doc to know more about TopoLVM
  CSI
- Any new resource units should also implement `resourceManager` interface

### Lifecycle of Custom Resources

- [LVMCluster CR][lvmcluster] represents the volume groups that should be
  created and managed across nodes with custom node selector, toleration and
  device selectors
- Should be created and edited by user in operator installed namespace
- Only a single CR instance with a single volume group is supported.
- The user can choose to specify the devices to be used for the volumegroup. 
- All available disks will be used if no devicePaths are specified,.
- All fields in `status` are updated based on the status of volume groups
  creation across nodes

Note:
- Device Class and Volume Group can be read interchangeably
- Multiple CRs exist to separate concerns of which component deployed by LVMO
  updates which CR there by reducing multiple reconcile loops and colliding
  requests/updates to Kubernetes API Server
- Feel free to raise a github [issue][issue] for open discussions about API
  changes if required

[topolvm-repo]: https://github.com/topolvm/topolvm
[topolvm-design]: https://github.com/topolvm/topolvm/blob/main/docs/design.md
[controller]: ../../controllers/lvmcluster_controller.go
[lvmcluster]: ../../api/v1alpha1/lvmcluster_types.go
[issue]: https://github.com/red-hat-storage/lvm-operator/issues
