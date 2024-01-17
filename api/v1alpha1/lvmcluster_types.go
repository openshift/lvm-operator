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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LVMClusterSpec defines the desired state of LVMCluster
type LVMClusterSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Tolerations to apply to nodes to act on
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Storage describes the deviceClass configuration for local storage devices
	// +Optional
	Storage Storage `json:"storage,omitempty"`
}

type ThinPoolConfig struct {
	// Name of the thin pool to be created
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// SizePercent represents percentage of remaining space in the volume group that should be used
	// for creating the thin pool.
	// +kubebuilder:default=90
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=90
	SizePercent int `json:"sizePercent,omitempty"`

	// OverProvisionRatio is the factor by which additional storage can be provisioned compared to
	// the available storage in the thin pool.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Required
	// +required
	OverprovisionRatio int `json:"overprovisionRatio"`
}

type DeviceFilesystemType string

const (
	FilesystemTypeExt4 DeviceFilesystemType = "ext4"
	FilesystemTypeXFS  DeviceFilesystemType = "xfs"
)

type DeviceClass struct {
	// Name of the class, the VG and possibly the storageclass.
	// Validations to confirm that this field can be used as metadata.name field in storageclass
	// ref: https://github.com/kubernetes/apimachinery/blob/de7147/pkg/util/validation/validation.go#L209
	// +kubebuilder:validation:MaxLength=245
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Name string `json:"name,omitempty"`

	// DeviceSelector is a set of rules that should match for a device to be included in the LVMCluster
	// +optional
	DeviceSelector *DeviceSelector `json:"deviceSelector,omitempty"`

	// NodeSelector chooses nodes on which to create the deviceclass
	// +optional
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`

	// ThinPoolConfig contains configurations for the thin-pool
	// +kubebuilder:validation:Required
	// +required
	ThinPoolConfig *ThinPoolConfig `json:"thinPoolConfig"`

	// Default is a flag to indicate whether the device-class is the default
	// +optional
	Default bool `json:"default,omitempty"`

	// FilesystemType sets the filesystem the device should use
	// +kubebuilder:validation:Enum=xfs;ext4;""
	// +kubebuilder:default=xfs
	// +optional
	FilesystemType DeviceFilesystemType `json:"fstype,omitempty"`
}

// DeviceSelector specifies the list of criteria that have to match before a device is assigned
type DeviceSelector struct {
	// MinSize is the minimum size of the device which needs to be included. Defaults to `1Gi` if empty
	// +optional
	// MinSize *resource.Quantity `json:"minSize,omitempty"`

	// A list of device paths which would be chosen for creating Volume Group.
	// For example "/dev/disk/by-path/pci-0000:04:00.0-nvme-1"
	// We discourage using the device names as they can change over node restarts.
	// +optional
	Paths []string `json:"paths,omitempty"`

	// A list of device paths which could be chosen for creating Volume Group.
	// For example "/dev/disk/by-path/pci-0000:04:00.0-nvme-1"
	// We discourage using the device names as they can change over node restarts.
	// +optional
	OptionalPaths []string `json:"optionalPaths,omitempty"`
}

// type DeviceClassConfig struct {
// 	LVMConfig *LVMConfig `json:"lvmConfig,omitempty"`
// }

// type LVMConfig struct {
// 	thinProvision bool `json:"thinProvision,omitempty"`
// }

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
	Conditions []Condition `json:"conditions,omitempty"`

	// DeviceClassStatuses describes the status of all deviceClasses
	DeviceClassStatuses []DeviceClassStatus `json:"deviceClassStatuses,omitempty"`
}

// Condition represents the state of the operator's
// reconciliation functionality.
// +k8s:deepcopy-gen=true
type Condition struct {
	Type ConditionType `json:"type" description:"type of condition ie. Available|Progressing|Degraded."`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`

	// +optional
	LastHeartbeatTime metav1.Time `json:"lastHeartbeatTime" description:"last time we got an update on a given condition"`

	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime" description:"last time the condition transit from one status to another"`
}

// ConditionType is the state of the operator's reconciliation functionality.
type ConditionType string

const (
	// ConditionAvailable indicates that the resources maintained by the operator,
	// is functional and available in the cluster.
	ConditionAvailable ConditionType = "Available"

	// ConditionProgressing indicates that the operator is actively making changes to the resources maintained by the
	// operator
	ConditionProgressing ConditionType = "Progressing"

	// ConditionDegraded indicates that the resources maintained by the operator are not functioning completely.
	// An example of a degraded state would be if not all pods in a deployment were running.
	// It may still be available, but it is degraded
	ConditionDegraded ConditionType = "Degraded"

	// ConditionUpgradeable indicates whether the resources maintained by the operator are in a state that is safe to upgrade.
	// When `False`, the resources maintained by the operator should not be upgraded and the
	// message field should contain a human readable description of what the administrator should do to
	// allow the operator to successfully update the resources maintained by the operator.
	ConditionUpgradeable ConditionType = "Upgradeable"
)

// DeviceClassStatus defines the observed status of the deviceclass across all nodes
type DeviceClassStatus struct {
	// Name is the name of the deviceclass
	Name string `json:"name,omitempty"`
	// NodeStatus tells if the deviceclass was created on the node
	NodeStatus []NodeStatus `json:"nodeStatus,omitempty"`
}

type Storage struct {
	// DeviceClasses are a rules that assign local storage devices to volumegroups that are used for creating lvm based PVs
	// +Optional
	DeviceClasses []DeviceClass `json:"deviceClasses,omitempty"`
}

// NodeStatus defines the observed state of the deviceclass on the node
type NodeStatus struct {
	// Node is the name of the node
	Node string `json:"node,omitempty"`
	// Status is the status of the VG on the node
	Status VGStatusType `json:"status,omitempty"`
	// Reason provides more detail on the VG creation status
	Reason string `json:"reason,omitempty"`
	// Devices is the list of devices used by the deviceclass
	Devices []string `json:"devices,omitempty"`
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
