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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
)

// NoPlaneControlPlaneSpec defines the desired state of NoPlaneControlPlane.
type NoPlaneControlPlaneSpec struct {
	// version is the desired Kubernetes version (e.g. "v1.29.2").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Version string `json:"version"`

	// replicas is the desired number of control plane replicas.
	// +optional
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// controlPlaneEndpoint is the endpoint of the provisioned control plane.
	// Populated by the controller once ready. May be set by the user for
	// brownfield adoption.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// credentialsSecretRef references the Secret containing the noplane.io API key.
	// The Secret must contain a key named "apiKey".
	// +required
	CredentialsSecretRef corev1.SecretReference `json:"credentialsSecretRef"`

	// planeID is the noplane.io plane ID.
	// When set by the user, the controller skips creation and adopts the
	// existing control plane. Used for brownfield adoption.
	// +optional
	PlaneID string `json:"planeID,omitempty"`
}

// NoPlaneControlPlaneStatus defines the observed state of NoPlaneControlPlane.
type NoPlaneControlPlaneStatus struct {
	// ready indicates the control plane is operational.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// initialized indicates the control plane has been successfully initialised
	// at least once. Once true, it is never set back to false.
	// CAPI core uses this to gate worker node bootstrap via CABPK.
	// +optional
	Initialized bool `json:"initialized,omitempty"`

	// planeID is the noplane.io ID of the provisioned control plane.
	// Persisted here to survive controller restarts (crash recovery).
	// +optional
	PlaneID string `json:"planeID,omitempty"`

	// version is the Kubernetes version currently running on the control plane.
	// +optional
	Version string `json:"version,omitempty"`

	// replicas is the total number of control plane replicas.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// readyReplicas is the number of ready replicas.
	// +optional
	ReadyReplicas *int32 `json:"readyReplicas,omitempty"`

	// selector is the label selector string for the scale subresource.
	// +optional
	Selector string `json:"selector,omitempty"`

	// conditions summarises the current state of the control plane.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=noplanecontrolplanes,shortName=npcp,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=".metadata.labels['cluster\\.x-k8s\\.io/cluster-name']"
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Initialized",type=boolean,JSONPath=".status.initialized"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// NoPlaneControlPlane is the Schema for the noplanecontrolplanes API.
type NoPlaneControlPlane struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NoPlaneControlPlane
	// +required
	Spec NoPlaneControlPlaneSpec `json:"spec"`

	// status defines the observed state of NoPlaneControlPlane
	// +optional
	Status NoPlaneControlPlaneStatus `json:"status,omitzero"`
}

// GetConditions returns the conditions for the NoPlaneControlPlane.
func (n *NoPlaneControlPlane) GetConditions() clusterv1.Conditions {
	return n.Status.Conditions
}

// SetConditions sets the conditions for the NoPlaneControlPlane.
func (n *NoPlaneControlPlane) SetConditions(conditions clusterv1.Conditions) {
	n.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// NoPlaneControlPlaneList contains a list of NoPlaneControlPlane.
type NoPlaneControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NoPlaneControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NoPlaneControlPlane{}, &NoPlaneControlPlaneList{})
}
