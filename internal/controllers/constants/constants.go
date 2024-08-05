/*
Copyright © 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package constants

const (
	TopolvmCSIDriverName = "topolvm.io"

	VGManagerServiceAccount = "vg-manager"

	VgManagerMemRequest = "45Mi"
	VgManagerCPURequest = "5m"

	// topoLVM Node
	CSIKubeletRootDir               = "/var/lib/kubelet/"
	TopolvmNodeContainerHealthzName = "healthz"

	DefaultCSISocket              = "/run/topolvm/csi-topolvm.sock"
	DeviceClassKey                = "topolvm.io/device-class"
	DefaultPluginRegistrationPath = "/registration"

	// name of the lvm-operator container
	LVMOperatorContainerName = "manager"

	// annotations

	// WorkloadPartitioningManagement contains the management workload annotation
	WorkloadPartitioningManagementAnnotation = "target.workload.openshift.io/management"
	ManagementAnnotationVal                  = `{"effect": "PreferredDuringScheduling"}`

	// DevicesWipedAtAnnotation is an annotation that marks when a device has been wiped
	DevicesWipedAtAnnotation = "devices.lvms.openshift.io/wiped-at"

	// labels and values

	// AppKubernetesPartOfLabel is the Kubernetes recommended part-of label
	AppKubernetesPartOfLabel = "app.kubernetes.io/part-of"
	// AppKubernetesNameLabel is the Kubernetes recommended name label
	AppKubernetesNameLabel = "app.kubernetes.io/name"
	// AppKubernetesManagedByLabel is the Kubernetes recommended managed-by label
	AppKubernetesManagedByLabel = "app.kubernetes.io/managed-by"
	// AppKubernetesComponentLabel is the Kubernetes recommended component label
	AppKubernetesComponentLabel = "app.kubernetes.io/component"

	VGManagerLabelVal = "vg-manager"
	ManagedByLabelVal = "lvms-operator"
	PartOfLabelVal    = "lvms-provisioner"

	StorageClassPrefix        = "lvms-"
	VolumeSnapshotClassPrefix = "lvms-"
	SCCPrefix                 = "lvms-"

	PriorityClassNameUserCritical    = "openshift-user-critical"
	PriorityClassNameClusterCritical = "system-cluster-critical"
)

// these constants are derived from the TopoLVM recommendations but maintained separately to allow easy override.
// see https://github.com/topolvm/topolvm/blob/a967c95da14f80955332a00ebb258e319c6c39ac/cmd/topolvm-controller/app/root.go#L17-L28
const (
	// DefaultMinimumAllocationSizeBlock is the default minimum size for a block volume.
	// Derived from the usual physical extent size of 4Mi * 2 (for accommodating metadata)
	DefaultMinimumAllocationSizeBlock = "8Mi"
	// DefaultMinimumAllocationSizeXFS is the default minimum size for a filesystem volume with XFS formatting.
	// Derived from the hard XFS minimum size of 300Mi that is enforced by the XFS filesystem.
	DefaultMinimumAllocationSizeXFS = "300Mi"
	// DefaultMinimumAllocationSizeExt4 is the default minimum size for a filesystem volume with ext4 formatting.
	// Derived from the usual 4096K blocks, 1024 inode default and journaling overhead,
	// Allows for more than 80% free space after formatting, anything lower significantly reduces this percentage.
	DefaultMinimumAllocationSizeExt4 = "32Mi"
)
