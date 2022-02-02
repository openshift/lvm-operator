This page outlines `spec` and `status` fields of all the CRs managed by LVMO

## 1. LVMCluster

- LVMCluster CR represents the volume groups that should be created and managed
  across nodes with custom node selector, toleration and device selectors
- Should be created and edited by user in operator installed namespace and only
  single CR is supported, creation of multiple CRs is erroneous

| Field    | Type             | Description                                                      |
|----------|------------------|------------------------------------------------------------------|
| `spec`   | LVMClusterSpec   | Desired behaviour of LVM Cluster with Volume Groups across Nodes |
| `status` | LVMClusterStatus | Observed status of LVM Cluster with Volume Groups across Nodes   |

### LVMClusterSpec

| Field           | Type                | Description                                                                        |
|-----------------|---------------------|------------------------------------------------------------------------------------|
| `toleraions`    | []corev1.Toleration | List of tolerations that Topolvm Controller, Node Plugin and VG Manager can run on |
| `deviceClasses` | []DeviceClass       | List of deviceclasses                                                              |

#### DeviceClass

| Field            | Type                  | Description                           |
|------------------|-----------------------|---------------------------------------|
| `name`           | string                | Name of the Volume Group              |
| `deviceSelector` | \*DeviceSelector      | Selector to match devices on the node |
| `nodeSelector`   | \*corev1.NodeSelector | Selector to match nodes               |

#### DeviceSelector

- Not Implemented, empty struct

### LVMClusterStatus

| Field                 | Type                | Description                           |
|-----------------------|---------------------|---------------------------------------|
| `ready`               | bool                | State of the LVMCluster               |
| `deviceClassStatuses` | []DeviceClassStatus | List of observed deviceClass statuses |

#### DeviceClassStatus

| Field        | Type         | Description                                          |
|--------------|--------------|------------------------------------------------------|
| `name`       | string       | Name of the device class                             |
| `nodeStatus` | []NodeStatus | Status of the node that belongs to this device class |

#### NodeStatus

| Field     | Type         | Description                                                           |
|-----------|--------------|-----------------------------------------------------------------------|
| `Node`    | string       | Name of the node                                                      |
| `status`  | VGStatusType | One of `Ready`, `Failed`, `Degraded`                                  |
| `devices` | []string     | List of Physical Volumes (devices) that are part of this device class |

### Lifecycle

1. Only a single volume group containing all available disks across schedulable
   nodes is supported and implementation respecting tolerations, device andi
   node selector fields is coming soon
2. All fields in `status` is updated based on the status of volume groups
   creation across nodes
3. `LVMCluster` is created with a [finalizer][finalizer] and on deletion of CR,
   [entities][reconciler] created by LVMO will be deleted

## 2. LVMVolumeGroup

| Field    | Type                 | Description                                 |
|----------|----------------------|---------------------------------------------|
| `spec`   | LVMVolumeGroupSpec   | Desired Volume Groups on respective nodes   |
| `status` | LVMVolumeGroupStatus | Status of Volume Groups on respective nodes |

### LVMClusterSpec

| Field            | Type                  | Description                           |
|------------------|-----------------------|---------------------------------------|
| `deviceSelector` | \*DeviceSelector      | Selector to match devices on the node |
| `nodeSelector`   | \*corev1.NodeSelector | Selector to match nodes               |

### LVMVolumeGroupStatus

- Not Implemented, empty struct

### Lifecycle

1. Created and managed by LVMO, this contains subset of fields from LVMCluster
   CR. Volume Groups with same name can exist on different nodes and to
   reconcile based on this CR reduces the burden for multiple controllers

## 3. LVMVolumeGroupNodeStatus

| Field    | Type                           | Description                          |
|----------|--------------------------------|--------------------------------------|
| `spec`   | LVMVolumeGroupNodeStatusSpec   | Status of Volume Groups across nodes |
| `status` | LVMVolumeGroupNodeStatusStatus | Status of Custom Resource            |

### LVMVolumeGroupNodeStatusSpec

| Field        | Type                           | Description                        |
|--------------|--------------------------------|------------------------------------|
| `nodeStatus` | []VGStatus                     | Contains per node status of the VG |

### VGStatus

| Field     | Type         | Description                                                           |
|-----------|--------------|-----------------------------------------------------------------------|
| `name`    | string       | Name of the Volume Group                                              |
| `status`  | VGStatusType | One of `Ready`, `Failed`, `Degraded`                                  |
| `reason`  | string       | Provides more detail on Volume Group status                           |
| `devices` | []string     | List of Physical Volumes (devices) that are part of this Volume Group |

1. Created and managed by LVMO, this contains subset of fields from LVMCluster
   CR. Captures the status of volume groups across nodes

## Notes:

1. Device Class and Volume Group can be read interchangeably
2. Multiple CRs exist to separate concerns of which component deployed by LVMO
   updates which CR there by reducing multiple reconcile loops and colliding
   requests/updates to Kubernetes API Server
3. Feel free to raise a github [issue][issue] for open discussions about API
   changes if required

[finalizer]: https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/#finalizers
[reconciler]: ./reconciler.md
[issue]: https://github.com/red-hat-storage/lvm-operator/issues
