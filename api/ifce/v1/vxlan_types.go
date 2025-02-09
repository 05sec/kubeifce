/*
Copyright 2025.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VxlanSpec defines the desired state of Vxlan.
type VxlanSpec struct {
	// VXLAN Network Identifier (VNI)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=16777215
	VNI int `json:"vni"`

	// Master interface name
	// +kubebuilder:validation:Optional
	Master *string `json:"master,omitempty"`

	// MTU size for the VXLAN interface
	// defaults to 1450
	// +kubebuilder:validation:Optional
	MTU *int `json:"mtu,omitempty"`

	// Group IP address for VXLAN tunnel
	// +kubebuilder:validation:Optional
	GroupIP *string `json:"groupIP,omitempty"`

	// Remote IP address for VXLAN tunnel
	// +kubebuilder:validation:Optional
	RemoteIP *string `json:"remoteIP,omitempty"`

	// Local IP address for VXLAN tunnel
	// +kubebuilder:validation:Optional
	LocalIP *string `json:"localIP,omitempty"`

	// TTL for VXLAN packets
	// +kubebuilder:validation:Optional
	TTL *int `json:"ttl,omitempty"`

	// UDP port for VXLAN
	// defaults to 4789
	// +kubebuilder:validation:Optional
	Port *int `json:"port,omitempty"`
}

// VxlanStatus defines the observed state of Vxlan.
type VxlanStatus struct {
	// Current state of the VXLAN interface (up/down)
	State string `json:"state"`

	// Interface name
	Name string `json:"name"`

	// Represents the observations of a VXLAN's current state.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Vxlan is the Schema for the vxlans API.
type Vxlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VxlanSpec   `json:"spec,omitempty"`
	Status VxlanStatus `json:"status,omitempty"`
}

// VxlanList contains a lis`t of Vxlan.
// +kubebuilder:object:root=true
type VxlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Vxlan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Vxlan{}, &VxlanList{})
}
