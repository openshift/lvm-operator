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

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LVMVolumeGroupSpec defines the desired state of LVMVolumeGroup
type LVMVolumeGroupSpec struct {
	// DeviceDiscoveryPolicy is a flag to indicate whether the device should be discovered
	// at install time or at runtime (static or dynamic configuration of devices)
	// If set to DeviceDiscoveryPolicyInstallStatic, the devices will not be added to the VG if they are not present at LVMCluster creation time.
	// If set to DeviceDiscoveryPolicyRuntimeDynamic, the devices will be added to the VG if they are present at runtime.
	// By default, the value is set to RuntimeDynamic.
	// This field cannot be updated once the LVMCluster is created.
	// +kubebuilder:validation:Enum=InstallStatic;RuntimeDynamic
	// +kubebuilder:default=RuntimeDynamic
	// +kubebuilder:validation:Required
	DeviceDiscoveryPolicy DeviceDiscoveryPolicy `json:"deviceDiscoveryPolicy,omitempty"`

	// DeviceSelector is a set of rules that should match for a device to be included in this TopoLVMCluster
	// +optional
	DeviceSelector *DeviceSelector `json:"deviceSelector,omitempty"`

	// NodeSelector chooses nodes
	// +optional
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`

	// ThinPoolConfig contains configurations for the thin-pool
	// +kubebuilder:validation:Required
	// +required
	ThinPoolConfig *ThinPoolConfig `json:"thinPoolConfig"`

	// Default is a flag to indicate whether the device-class is the default
	// +optional
	Default bool `json:"default,omitempty"`
}

// LVMVolumeGroupStatus defines the observed state of LVMVolumeGroup
type LVMVolumeGroupStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// LVMVolumeGroup is the Schema for the lvmvolumegroups API
type LVMVolumeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LVMVolumeGroupSpec   `json:"spec,omitempty"`
	Status LVMVolumeGroupStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LVMVolumeGroupList contains a list of LVMVolumeGroup
type LVMVolumeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LVMVolumeGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LVMVolumeGroup{}, &LVMVolumeGroupList{})
}
