/*
Copyright 2021 Red Hat Openshift Data Foundation.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LVMVolumeGroupNodeStatusSpec defines the desired state of LVMVolumeGroupNodeStatus
type LVMVolumeGroupNodeStatusSpec struct {
	// NodeStatus contains the per node status of the VG
	LVMVGStatus []VGStatus `json:"nodeStatus,omitempty"`
}

type VGStatusType string

const (
	// VGStatusReady means that the vg has been created and is Ready
	VGStatusReady VGStatusType = "Ready"
	// VGStatusFailed means that the VG could not be created
	VGStatusFailed VGStatusType = "Failed"
	// VGStatusDegraded means that the VG has been created but is not using the specified config
	VGStatusDegraded VGStatusType = "Degraded"
)

type VGStatus struct {
	// Name is the name of the VG
	Name string `json:"name,omitempty"`
	// Status tells if the VG was created on the node
	Status VGStatusType `json:"status,omitempty"`
	// Reason provides more detail on the VG creation status
	Reason string `json:"reason,omitempty"`
	//Devices is the list of devices used by the VG
	Devices []string `json:"devices,omitempty"`
}

// LVMVolumeGroupNodeStatusStatus defines the observed state of LVMVolumeGroupNodeStatus
type LVMVolumeGroupNodeStatusStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// LVMVolumeGroupNodeStatus is the Schema for the lvmvolumegroupnodestatuses API
type LVMVolumeGroupNodeStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LVMVolumeGroupNodeStatusSpec   `json:"spec,omitempty"`
	Status LVMVolumeGroupNodeStatusStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LVMVolumeGroupNodeStatusList contains a list of LVMVolumeGroupNodeStatus
type LVMVolumeGroupNodeStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LVMVolumeGroupNodeStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LVMVolumeGroupNodeStatus{}, &LVMVolumeGroupNodeStatusList{})
}
