# Logical Volume Manager Storage (LVMS) Architecture

![component diagram](https://www.plantuml.com/plantuml/png/XP8nJyCm48Lt_ugdii3G0GH2oe3Q9eY5WjIACg0ELdAq5OvjsMSaXFZlk4c46apAUkzzB_SkddYMZaEjX9NbczmGHlUh-HAvgQtHfDcFy2c0bpZ5eoKdsRXrDyXLy4mEf_cYE5lZP5eKvxEBlRWoAjI4EsU2nLpg6Fn3jLehzSdKy60gMhBau7zRlqHlusJXtWe-ShFBwVNjLVC9izcLKg5r76WnizyJu_7DG1baACWgy-7_PDBhP7YMN6vfq9_U9JAv8yda8SJ06f5E6Xs2KbUe6xE7wcplhUr8P7A-h9Dz1sFJ24qyRtSQrXWrd9XMJCzod3t-AZ8ysMhVLuX_lOF_Pq6lYahsiH31DuoOaAv2hRu1)

The following table provides a view of the runners in these components:

| Component            | Kind       | Runners                                                                                             |
|----------------------|------------|-----------------------------------------------------------------------------------------------------|
| LVM Operator         | Deployment | [manager](lvm-operator-manager.md)                                                                  |
|                      |            | [topolvm-controller](https://github.com/topolvm/topolvm/blob/main/docs/topolvm-controller.md)       |
|                      |            | [csi-provisioner](https://github.com/kubernetes-csi/external-provisioner/blob/master/doc/design.md) |
|                      |            | [csi-resizer](https://github.com/kubernetes-csi/external-resizer#csi-resizer)                       |
|                      |            | [liveness-probe](https://github.com/kubernetes-csi/livenessprobe#liveness-probe)                    |
|                      |            | [csi-snapshotter](https://github.com/kubernetes-csi/external-snapshotter#csi-snapshotter)           |
| Volume Group Manager | DaemonSet  | [vg-manager](vg-manager.md)                                                                         |
|                      |            | [topolvm-node](https://github.com/topolvm/topolvm/blob/main/docs/topolvm-node.md)                   |
|                      |            | [csi-registrar](https://github.com/kubernetes-csi/node-driver-registrar#node-driver-registrar)      |
|                      |            | [liveness-probe](https://github.com/kubernetes-csi/livenessprobe#liveness-probe)                    |

This architecture diagram describes how the LVMS components work together to enable dynamic provisioning and management of logical volumes using Logical Volume Manager (LVM) in OpenShift clusters. See also [this page](https://github.com/topolvm/topolvm/blob/main/docs/design.md) for further details on the TopoLVM design.
