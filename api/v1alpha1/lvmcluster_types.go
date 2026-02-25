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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LVMClusterSpec defines the desired state of LVMCluster
type LVMClusterSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Tolerations to apply to nodes to act on
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Storage contains the device class configuration for local storage devices.
	// +Optional
	Storage Storage `json:"storage,omitempty"`
}

type ThinPoolConfig struct {
	// Name specifies a name for the thin pool.
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// SizePercent specifies the percentage of space in the LVM volume group for creating the thin pool.
	// If the size configuration is 100, the whole disk will be used.
	// By default, 90% of the disk is used for the thin pool to allow for data or metadata expansion later on.
	// +kubebuilder:default=90
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=100
	SizePercent int `json:"sizePercent,omitempty"`

	// OverProvisionRatio specifies a factor by which you can provision additional storage based on the available storage in the thin pool. To prevent over-provisioning through validation, set this field to 1.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:validation:Required
	// +required
	OverprovisionRatio int `json:"overprovisionRatio"`

	// ChunkSizeCalculationPolicy specifies the policy to calculate the chunk size for the underlying volume.
	// When set to Host, the chunk size is calculated based on the lvm2 host setting on the node.
	// When set to Static, the chunk size is calculated based on the static size attribute provided within ChunkSize.
	// +kubebuilder:default=Static
	// +kubebuilder:validation:Enum=Host;Static
	// +optional
	ChunkSizeCalculationPolicy ChunkSizeCalculationPolicy `json:"chunkSizeCalculationPolicy,omitempty"`

	// ChunkSize specifies the statically calculated chunk size for the thin pool.
	// Thus, It is only used when the ChunkSizeCalculationPolicy is set to Static.
	// No ChunkSize with a ChunkSizeCalculationPolicy set to Static will result in a default chunk size of 128Ki.
	// It can be between 64Ki and 1Gi due to the underlying limitations of lvm2.
	// +optional
	ChunkSize *resource.Quantity `json:"chunkSize,omitempty"`

	// MetadataSize specifies metadata size for thin pool. It used only when MetadataSizeCalculationPolicy
	// is set to Static. No MetadataSize with a MetadataSizeCalculationPolicy set to Static will result in
	// default metadata size of 1Gi. It can be between 2Mi and 16Gi due to the underlying limitations of lvm2.
	// +optional
	MetadataSize *resource.Quantity `json:"metadataSize,omitempty"`

	// MetadataSizeCalculationPolicy specifies the policy to calculate metadata size for the underlying volume.
	// When set to Host, the metadata size is calculated based on lvm2 default settings
	// When set to Static, the metadata size is calculated based on the static size attribute provided within MetadataSize
	// +kubebuilder:default=Host
	// +kubebuilder:validation:Enum=Host;Static
	// +optional
	MetadataSizeCalculationPolicy MetadataSizePolicy `json:"metadataSizeCalculationPolicy,omitempty"`
}

// MetadataSizePolicy specifies the policy to calculate the metadata size for the underlying volume.
type MetadataSizePolicy string

const (
	// MetadataSizePolicyHost calculates the metadata size based on the lvm2 default settings.
	MetadataSizePolicyHost MetadataSizePolicy = "Host"
	// MetadataSizePolicyStatic calculates the metadata size based on a static size attribute.
	MetadataSizePolicyStatic MetadataSizePolicy = "Static"
)

var (
	ThinPoolMetadataSizeMinimum = resource.MustParse("2Mi")
	ThinPoolMetadataSizeMaximum = resource.MustParse("16Gi")
	ThinPoolMetadataSizeDefault = resource.MustParse("1Gi")
)

// ChunkSizeCalculationPolicy specifies the policy to calculate the chunk size for the underlying volume.
// for more information, see man lvm.
type ChunkSizeCalculationPolicy string

const (
	// ChunkSizeCalculationPolicyHost calculates the chunk size based on the lvm2 host setting on the node.
	ChunkSizeCalculationPolicyHost ChunkSizeCalculationPolicy = "Host"
	// ChunkSizeCalculationPolicyStatic calculates the chunk size based on a static size attribute.
	ChunkSizeCalculationPolicyStatic ChunkSizeCalculationPolicy = "Static"
)

var (
	ChunkSizeMinimum = resource.MustParse("64Ki")
	ChunkSizeDefault = resource.MustParse("128Ki")
	ChunkSizeMaximum = resource.MustParse("1Gi")
)

type DeviceFilesystemType string

const (
	FilesystemTypeExt4 DeviceFilesystemType = "ext4"
	FilesystemTypeXFS  DeviceFilesystemType = "xfs"
)

type DeviceClass struct {
	// Name specifies a name for the device class
	// +kubebuilder:validation:MaxLength=245
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Name string `json:"name,omitempty"`

	// DeviceSelector contains the configuration to specify paths to the devices that you want to add to the LVM volume group, and force wipe the selected devices.
	// +optional
	DeviceSelector *DeviceSelector `json:"deviceSelector,omitempty"`

	// NodeSelector contains the configuration to choose the nodes on which you want to create the LVM volume group. If this field is not configured, all nodes without no-schedule taints are considered.
	// +optional
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`

	// ThinPoolConfig contains the configuration to create a thin pool in the LVM volume group. If you exclude this field, logical volumes are thick provisioned.
	// +optional
	ThinPoolConfig *ThinPoolConfig `json:"thinPoolConfig,omitempty"`

	// Default is a flag to indicate that a device class is the default. You can configure only a single default device class.
	// +optional
	Default bool `json:"default,omitempty"`

	// FilesystemType sets the default filesystem type for persistent volumes created from this device class.
	// This determines the filesystem used when provisioning PVCs with volumeMode: Filesystem.
	// Select either `ext4` or `xfs`. This does not filter devices during discovery.
	// +kubebuilder:validation:Enum=xfs;ext4;""
	// +kubebuilder:default=xfs
	// +optional
	FilesystemType DeviceFilesystemType `json:"fstype,omitempty"`

	// DeviceDiscoveryPolicy specifies the policy for discovering devices for this device class.
	// Static means the volume group is created with devices found at install time; new devices are ignored.
	// Dynamic means devices are continuously discovered and added to the volume group.
	// When not set, new volume groups default to Static and existing volume groups default to Dynamic for backward compatibility.
	// +kubebuilder:validation:Enum=Static;Dynamic
	// +optional
	DeviceDiscoveryPolicy *DeviceDiscoveryPolicySpec `json:"deviceDiscoveryPolicy,omitempty"`

	// StorageClassOptions allows customization of the StorageClass created for this device class.
	// +optional
	StorageClassOptions *StorageClassOptions `json:"storageClassOptions,omitempty"`
}

// StorageClassOptions defines optional overrides for the StorageClass generated by LVMS for a device class.
type StorageClassOptions struct {
	// ReclaimPolicy sets the reclaim policy for PVs provisioned by this device class.
	// When set to Retain, PVs and their underlying logical volumes are preserved when PVCs are deleted.
	// Defaults to Delete if not specified.
	// +optional
	// +kubebuilder:validation:Enum=Delete;Retain
	ReclaimPolicy *corev1.PersistentVolumeReclaimPolicy `json:"reclaimPolicy,omitempty"`

	// VolumeBindingMode sets the binding mode for PVs provisioned by this device class.
	// Defaults to WaitForFirstConsumer if not specified.
	// +optional
	// +kubebuilder:validation:Enum=WaitForFirstConsumer;Immediate
	VolumeBindingMode *storagev1.VolumeBindingMode `json:"volumeBindingMode,omitempty"`

	// AdditionalParameters sets additional parameters on the StorageClass.
	// LVMS-owned keys (topolvm.io/device-class, csi.storage.k8s.io/fstype) cannot be overridden.
	// This field is immutable after creation.
	// +optional
	AdditionalParameters map[string]string `json:"additionalParameters,omitempty"`

	// AdditionalLabels sets additional labels on the StorageClass.
	// This is the only StorageClassOptions field that can be changed after creation.
	// +optional
	AdditionalLabels map[string]string `json:"additionalLabels,omitempty"`
}

// DeviceSelector specifies the list of criteria that have to match before a device is assigned
type DeviceSelector struct {
	// MinSize is the minimum size of the device which needs to be included. Defaults to `1Gi` if empty
	// +optional
	// MinSize *resource.Quantity `json:"minSize,omitempty"`

	// Paths specify the device paths.
	// +optional
	Paths []DevicePath `json:"paths,omitempty"`

	// OptionalPaths specify the optional device paths.
	// +optional
	OptionalPaths []DevicePath `json:"optionalPaths,omitempty"`

	// ForceWipeDevicesAndDestroyAllData is a flag to force wipe the selected devices.
	// This wipes the file signatures on the devices. Use this feature with caution.
	// Force wipe the devices only when you know that they do not contain any important data.
	// +optional
	ForceWipeDevicesAndDestroyAllData *bool `json:"forceWipeDevicesAndDestroyAllData,omitempty"`
}

type DevicePath string

func (d DevicePath) Unresolved() string {
	return string(d)
}

type LVMStateType string

const (
	// LVMStatusProgressing means that the lvmcluster creation is in progress
	LVMStatusProgressing LVMStateType = "Progressing"
	// LVMStatusReady means that the lvmcluster has been created and is Ready
	LVMStatusReady LVMStateType = "Ready"
	// LVMStatusFailed means that the lvmcluster could not be created
	LVMStatusFailed LVMStateType = "Failed"
	// LVMStatusDegraded means that the lvmcluster has been created but is not using the specified config
	LVMStatusDegraded LVMStateType = "Degraded"
	// LVMStatusUnknown means that the lvmcluster has been in an unknown state
	LVMStatusUnknown LVMStateType = "Unknown"
)

// LVMClusterStatus defines the observed state of LVMCluster
type LVMClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Ready describes if the LVMCluster is ready.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// State describes the LVMCluster state.
	// +optional
	State LVMStateType `json:"state,omitempty"`

	// Conditions describes the state of the resource.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DeviceClassStatuses describes the status of all deviceClasses
	DeviceClassStatuses []DeviceClassStatus `json:"deviceClassStatuses,omitempty"`
}

const (
	// ResourcesAvailable indicates whether the resources maintained by the operator are reconciled without any issues.
	ResourcesAvailable = "ResourcesAvailable"

	// VolumeGroupsReady indicates whether the volume groups maintained by the operator are in a ready state.
	VolumeGroupsReady = "VolumeGroupsReady"
)

// DeviceClassStatus defines the observed status of the deviceclass across all nodes
type DeviceClassStatus struct {
	// Name is the name of the deviceclass
	Name string `json:"name,omitempty"`
	// NodeStatus tells if the deviceclass was created on the node
	NodeStatus []NodeStatus `json:"nodeStatus,omitempty"`
}

type Storage struct {
	// DeviceClasses contains the configuration to assign the local storage devices to the LVM volume groups that you can use to provision persistent volume claims (PVCs).
	// +Optional
	DeviceClasses []DeviceClass `json:"deviceClasses,omitempty"`
}

// NodeStatus defines the observed state of the deviceclass on the node
type NodeStatus struct {
	// Node is the name of the node
	Node     string `json:"node,omitempty"`
	VGStatus `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// LVMCluster is the Schema for the lvmclusters API
type LVMCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LVMClusterSpec   `json:"spec,omitempty"`
	Status LVMClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LVMClusterList contains a list of LVMCluster
type LVMClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LVMCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LVMCluster{}, &LVMClusterList{})
}
