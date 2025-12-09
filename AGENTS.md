This file provides guidance to AI agents when working with code in this repository.

This is the LVM Operator repository - part of the Logical Volume Manager Storage (LVMS) solution for OpenShift. It contains:

- Kubernetes operator for managing logical volume storage
- Custom Resource Definitions (LVMCluster, LVMVolumeGroup, LVMVolumeGroupNodeStatus)
- Integration with TopoLVM CSI Driver for dynamic volume provisioning
- Volume Group Manager for node-level LVM operations
- End-to-end test suite for LVMS functionality

## Key Architecture Components

### LVMCluster Custom Resource
The primary API for users to configure logical volume storage. It defines:
- Device classes and device selectors for identifying available disks
- Volume group configurations and thin pool settings
- Storage class and volume snapshot class configurations
- TopoLVM CSI driver deployment parameters

The operator watches LVMCluster resources and reconciles the desired state by deploying TopoLVM components and managing volume groups across cluster nodes.

### Volume Group Manager
The `vg-manager` component runs as a DaemonSet on each node and is responsible for:
- Discovering available block devices matching the device selector
- Creating and managing LVM physical volumes and volume groups
- Managing thin pools for efficient storage provisioning
- Updating LVMVolumeGroupNodeStatus CRs with node-level status

### TopoLVM CSI Driver
A forked version of the upstream TopoLVM CSI driver (`github.com/openshift/topolvm`) that provides:
- Dynamic provisioning of logical volumes
- Topology-aware scheduling to place pods on nodes with sufficient storage
- Support for volume snapshots and clones (experimental, limited to single node)
- Thin provisioning capabilities for overcommitment

### Controller Architecture
The operator uses controller-runtime and consists of several reconcilers:
- `LVMCluster` controller: Manages TopoLVM deployment and storage configuration
- `LVMVolumeGroup` controller: Monitors volume group status across nodes
- Node-level controllers for device management and volume group operations

## Common Development Commands

### Building
```bash
make build              # Build the operator binary
make docker-build       # Build container image
make docker-push        # Push container image to registry
```

### Code Generation
```bash
make generate           # Generate code (deepcopy, etc.)
make manifests          # Generate CRD and RBAC manifests
```

After modifying API types in `api/`, always run both `make generate` and `make manifests` to update generated code and manifests.

### Deployment
```bash
# Standard deployment (uses kustomize)
make deploy             # Deploy operator to current cluster context
make undeploy           # Remove operator from cluster

# OLM-based deployment
make bundle             # Generate OLM bundle manifests
make bundle-build       # Build bundle image
make bundle-push        # Push bundle image to registry
make catalog-build      # Build catalog image
make catalog-push       # Push catalog image to registry
make deploy-with-olm    # Deploy using Operator Lifecycle Manager
make undeploy-with-olm    # Remove operator from cluster using Operator Lifecycle Manager
```

### Environment Variables for Deployment
```bash
export IMAGE_REGISTRY=quay.io           # Container registry (quay.io, docker.io, etc.)
export REGISTRY_NAMESPACE=myusername    # Your registry namespace/username
export IMAGE_TAG=v4.18-dev              # Image tag for built images
export OPERATOR_NAMESPACE=openshift-lvm-storage  # Namespace where operator runs (default)
```

### Testing
```bash
make test               # Run unit tests

# E2E tests require a live cluster
make deploy-local       # Build, push, and deploy local changes
make e2e                # Run end-to-end tests against deployed operator
```

To run e2e tests:
1. Set `IMAGE_REGISTRY` and `REGISTRY_NAMESPACE` environment variables
2. Run `make deploy-local` to build and deploy your changes
3. Wait for operator pod to be running: `oc -n openshift-lvm-storage get pods`
4. Run `make e2e` to execute the test suite
5. Clean up with `make undeploy`

### Validation and Verification
```bash
make fmt                # Run go fmt on all Go files
make vet                # Run go vet
make verify             # Verify go formatting and generated files
```

## Adding New APIs or Modifying Existing CRDs

### Modifying LVMCluster or Other CRDs
1. Edit the type definition in `api/v1alpha1/*_types.go`
2. Add appropriate kubebuilder markers for validation, defaults, etc.
3. Run `make generate` to update generated code (deepcopy methods)
4. Run `make manifests` to regenerate CRD YAML files
5. Update or add controller logic to handle the new field
6. Add unit tests for validation and controller behavior
7. Add e2e tests if the change affects user workflows
8. Update documentation in `docs/` or code comments

### Validation Markers
Use kubebuilder markers to add validation to CRD fields:
```go
// DeviceSelector specifies the criteria for selecting block devices.
// When not specified, all available and supported devices are discovered and added to the volume group.
// +optional
// +kubebuilder:validation:Optional
type DeviceSelector struct {
    // paths is a list of device paths which should be used for creating volume groups.
    // Paths must be absolute paths beginning with "/dev/".
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:validation:MinItems=1
    Paths []string `json:"paths,omitempty"`

    // optionalPaths is similar to paths but optional devices are allowed to be absent.
    // +optional
    // +kubebuilder:validation:Optional
    OptionalPaths []string `json:"optionalPaths,omitempty"`
}
```

All validation constraints must be documented in the field's comment.

### CRD Version Policy
- Current stable API version: `v1alpha1` (despite the name, this is the production API)
- New fields should generally be optional to maintain backward compatibility
- Breaking changes require careful consideration and migration support

## Testing Framework

### Unit Tests
Located alongside source files (e.g., `internal/controllers/*_test.go`). Use Ginkgo/Gomega for behavior-driven tests:
```go
var _ = Describe("LVMCluster Controller", func() {
    Context("When reconciling a new LVMCluster", func() {
        It("Should create the TopoLVM deployment", func() {
            // Test implementation
        })
    })
})
```

### E2E Tests
Located in `test/e2e/`. Tests real cluster scenarios:
- Creating LVMCluster with various configurations
- Provisioning PVCs using LVMS storage class
- Testing volume snapshots and clones
- Validating device discovery and volume group creation

E2E tests require:
- A real Kubernetes/OpenShift cluster with available block devices
- Cluster admin permissions
- The operator deployed from your local build

### Integration Tests (Migration of QE tests into this folder is still in progress)
The `test/` directory contains integration tests that verify:
- Controller reconciliation logic
- RBAC permissions
- Webhook validation
- Metrics collection

## Working with LVM and Storage

### Important Safety Considerations
This operator manages physical storage devices and performs destructive operations:
- **Data Loss Risk**: LVM operations can wipe disks. Always verify device selectors carefully.
- **Idempotency**: Controllers must handle partial states and retries safely.
- **Node Operations**: VG Manager runs privileged operations on nodes.
- **Cleanup**: Finalizers ensure proper cleanup, including removing volume groups.

### Device Selection
The operator filters out unsafe devices automatically:
- Read-only devices
- Devices with existing filesystems (unless LVM2_member with no children)
- Devices with partitions labeled as boot/bios/reserved
- ROM devices and existing LVM partitions
- Loop devices in use by Kubernetes

See "Unsupported Device Types" in the README for complete filter list.

### LVM Operations
The VG Manager performs LVM commands on nodes:
- `vgcreate`, `vgextend`: Manage volume groups
- `lvcreate`: Create thin pools
- Device wiping with `wipefs` before use if `forceWipeDevicesAndDestroyAllData` field is enabled in the API

All operations are logged and errors are reported via CR status conditions.

## Container-based Development

For consistency, you can run builds and tests in containers:
```bash
# Using podman (default)
make docker-build

# Using docker
make docker-build IMAGE_BUILD_CMD=docker
```

The operator image is based on UBI (Universal Base Image) for OpenShift compatibility.

## Debugging and Troubleshooting

### Checking Operator Status
```bash
# View operator pods
oc get pods -n openshift-lvm-storage

# Check operator logs
oc logs -n openshift-lvm-storage deployment/lvms-operator -c manager

# View LVMCluster status
oc get lvmcluster -A -o yaml

# Check volume group status on nodes
oc get lvmvolumegroupnodestatus -A
```

### Common Issues
1. **LVMCluster stuck in pending**: Check operator logs and events
2. **No devices found**: Verify device selector and check device filters
3. **VG Manager not running**: Check DaemonSet status and node selectors
4. **PVC stuck pending**: Ensure TopoLVM CSI driver is running, check storage class

See `docs/troubleshooting.md` for detailed troubleshooting guide.

## Monitoring and Metrics

LVMS exposes Prometheus metrics:
- TopoLVM metrics (volume capacity, provisioning duration, etc.)
- Controller-runtime metrics (reconciliation rate, queue depth, etc.)

Enable cluster monitoring:
```bash
oc patch namespace/openshift-lvm-storage -p '{"metadata": {"labels": {"openshift.io/cluster-monitoring": "true"}}}'
```

Access metrics via OpenShift Console → Observe → Metrics.

## Release and Versioning

- The operator follows semantic versioning
- Bundle versions align with OpenShift versions (4.x)
- CSV (ClusterServiceVersion) is generated for OLM deployments
- Image tags should include OpenShift version for production builds

## Known Limitations

Be aware of these limitations when developing:
- **Single LVMCluster**: Only one LVMCluster CR is supported per cluster
- **No Multi-Node Snapshots**: Snapshots/clones work only on the same node as source
- **No LVM RAID**: Use mdraid instead for redundancy
- **Dynamic Discovery**: Not recommended for production (use explicit device paths)
- **No Upgrades from 4.10/4.11**: Breaking API changes prevent upgrades

## Contributing

See `CONTRIBUTING.md` for:
- Code review process
- Commit message format
- PR submission guidelines
- Community standards

For the latest information about usage and installation of LVMS (Logical Volume Manager Storage) in OpenShift, use the official product documentation (`https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/storage/configuring-persistent-storage#persistent-storage-using-lvms`).
