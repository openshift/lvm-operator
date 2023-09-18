# Logical Volume Manager Storage (LVMS) Architecture

![component diagram](http://www.plantuml.com/plantuml/png/VLDDIyD04BtlhnZgHGyzA8gGWxHDn8jLQBKUf8SbcQR1pSwIdHH4_Eysss087VOwxoDltcHdddN3RMsKSZh_qYN2v7cpN4DAjIEBblq4VXJ0vt4AhmuRpTHi-q5gMi_Om6MwogwsS37Fikl5JGTkoBGrmbD3hOEbjaVZVzK92v2W71DUgC0rQswzG7qZHrsib2mtP4pun33kj5lrEzxiRB5HL7_qNzpExn_lGXGggrmRE346hFCSzm7JwOEO1nB8q1dwzb55Y1hdYfN6DTAD4lZGdEzHvilNII1jK3DwK6eKEQY4dWQ1jWNK8Qi7qzCE9vfIyafICuEXETG5v6HtLGcxoc34vEoqIG_xFWAK0GWXULzPS4J6ouvYGKAfKMtypqxWtHMQGpDnRkIwAzmPpDa3xn5yq2WrGrjqR_mF)

The following table provides a view of the containers in these components:

| Component            | Kind       | Containers                                                                                          |
|----------------------|------------|-----------------------------------------------------------------------------------------------------|
| LVM Operator         | Deployment | [manager](lvm-operator-manager.md)                                                                  |
|                      |            | [kube-rbac-proxy](https://github.com/brancz/kube-rbac-proxy#kube-rbac-proxy)                        |
| Volume Group Manager | DaemonSet  | [vg-manager](vg-manager.md)                                                                         |
| TopoLVM Controller   | Deployment | [topolvm-controller](https://github.com/topolvm/topolvm/blob/main/docs/topolvm-controller.md)       |
|                      |            | [csi-provisioner](https://github.com/kubernetes-csi/external-provisioner/blob/master/doc/design.md) |
|                      |            | [csi-resizer](https://github.com/kubernetes-csi/external-resizer#csi-resizer)                       |
|                      |            | [liveness-probe](https://github.com/kubernetes-csi/livenessprobe#liveness-probe)                    |
|                      |            | [csi-snapshotter](https://github.com/kubernetes-csi/external-snapshotter#csi-snapshotter)           |
| TopoLVM Node         | DaemonSet  | [topolvm-node](https://github.com/topolvm/topolvm/blob/main/docs/topolvm-node.md)                   |
|                      |            | [lvmd](https://github.com/topolvm/topolvm/blob/main/docs/lvmd.md)                                   |
|                      |            | [csi-registrar](https://github.com/kubernetes-csi/node-driver-registrar#node-driver-registrar)      |
|                      |            | [liveness-probe](https://github.com/kubernetes-csi/livenessprobe#liveness-probe)                    |

This architecture diagram describes how the LVMS components work together to enable dynamic provisioning and management of logical volumes using Logical Volume Manager (LVM) in OpenShift clusters. See also [this page](https://github.com/topolvm/topolvm/blob/main/docs/design.md) for further details on the TopoLVM design.
