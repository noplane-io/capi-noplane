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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// NoPlaneControlPlaneTemplateSpec defines the desired state of NoPlaneControlPlaneTemplate.
type NoPlaneControlPlaneTemplateSpec struct {
	// template defines the control plane template.
	// +required
	Template NoPlaneControlPlaneTemplateResource `json:"template"`
}

// NoPlaneControlPlaneTemplateResource defines the template structure.
type NoPlaneControlPlaneTemplateResource struct {
	// metadata is the metadata applied to the control plane created from this template.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of the control plane.
	// +required
	Spec NoPlaneControlPlaneSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=noplanecontrolplanetemplates,shortName=npcpt,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// NoPlaneControlPlaneTemplate is the Schema for the noplanecontrolplanetemplates API.
type NoPlaneControlPlaneTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NoPlaneControlPlaneTemplate
	// +required
	Spec NoPlaneControlPlaneTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// NoPlaneControlPlaneTemplateList contains a list of NoPlaneControlPlaneTemplate.
type NoPlaneControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NoPlaneControlPlaneTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NoPlaneControlPlaneTemplate{}, &NoPlaneControlPlaneTemplateList{})
}
