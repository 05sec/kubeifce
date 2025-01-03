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

// VlanSpec defines the desired state of Vlan.
type VlanSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// NodeName that the VLAN interface is created on
	// +kubebuilder:validation:Required
	NodeName string `json:"nodeName"`

	// Name of the VLAN interface
	// defaults format: ki.<master>.<vlan-ID>
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9-]+$"
	Name *string `json:"name,omitempty"`

	// VLAN ID (1-4094)
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4094
	ID *int `json:"id,omitempty"`

	// Master interface name
	// +kubebuilder:validation:Required
	Master *string `json:"master,omitempty"`

	// MTU size for the VLAN interface
	// defaults to 1496
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=68
	// +kubebuilder:validation:Maximum=8996
	MTU *int `json:"mtu,omitempty"`
}

// VlanStatus defines the observed state of Vlan.
type VlanStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Current state of the VLAN interface (up/down)
	State string `json:"state"`

	Name string `json:"name"`
}

// Vlan is the Schema for the vlans API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Vlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VlanSpec   `json:"spec,omitempty"`
	Status VlanStatus `json:"status,omitempty"`
}

// VlanList contains a list of Vlan.
// +kubebuilder:object:root=true
type VlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Vlan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Vlan{}, &VlanList{})
}
