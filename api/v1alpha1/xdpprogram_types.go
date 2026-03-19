/*
Copyright 2026.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// XdpProgramSpec defines the desired state of XdpProgram
type XdpProgramSpec struct {
	// Interface is the name of the target network interface to attach the XDP program to (e.g., "eth0").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Interface string `json:"interface"`

	// NodeName is the name of the Kubernetes node where this XDP program should be attached.
	// The operator's controller running on this node will reconcile this resource.
	// This field is required.
	// +kubebuilder:validation:Required
	NodeName string `json:"nodeName"`

	// BpfPath is the absolute path on the node to the compiled eBPF bytecode object file.
	// +kubebuilder:validation:Required
	BpfPath string `json:"bpfPath"`

	// Mode defines the XDP attach mode.v
	// - "generic": Generic mode, used when the driver lacks native support. Lower performance.
	// - "native": Native driver mode, offers high performance and requires network driver support.
	// - "offload": Hardware offload mode, provides the highest performance and requires network hardware support.
	// +kubebuilder:validation:Enum=generic;native;offload
	// +kubebuilder:default:=generic
	// +optional
	Mode string `json:"mode,omitempty"`
}

// XdpProgramStatus defines the observed state of XdpProgram.
type XdpProgramStatus struct {
	// Ready indicates whether the XDP program has been successfully attached on the target node.
	Ready bool `json:"ready"`

	// LoadedAt records the timestamp of the last successful program attachment.
	// +optional
	LoadedAt *metav1.Time `json:"loadedAt,omitempty"`

	// Message provides detailed status information, including any error messages.
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.message"
// +kubebuilder:printcolumn:name="Interface",type="string",JSONPath=".spec.interface"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// XdpProgram is the Schema for the xdpprograms API
type XdpProgram struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of XdpProgram
	// +required
	Spec XdpProgramSpec `json:"spec"`

	// status defines the observed state of XdpProgram
	// +optional
	Status XdpProgramStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// XdpProgramList contains a list of XdpProgram
type XdpProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []XdpProgram `json:"items"`
}

func init() {
	SchemeBuilder.Register(&XdpProgram{}, &XdpProgramList{})
}
