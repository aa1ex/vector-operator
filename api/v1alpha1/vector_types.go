/*
Copyright 2022.

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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VectorSpec defines the desired state of Vector
type VectorSpec struct {
	// DisableAggregation
	// DisableAggregation bool `json:"disableAggregation,omitempty"`
	// Vector Agent
	Agent *VectorAgent `json:"agent,omitempty"`
	// Determines if requests to the kube-apiserver can be served by a cache.
	UseApiServerCache bool `json:"useApiServerCache,omitempty"`
}

// VectorStatus defines the observed state of Vector
type VectorStatus struct {
	VectorCommonStatus `json:",inline"`
}

// VectorAgent is the Schema for the Vector Agent
type VectorAgent struct {
	VectorCommon `json:",inline"`
}

// ApiSpec is the Schema for the Vector Agent GraphQL API - https://vector.dev/docs/reference/api/
type ApiSpec struct {
	Enabled    bool `json:"enabled,omitempty"`
	Playground bool `json:"playground,omitempty"`
	// Enable ReadinessProbe and LivenessProbe via api /health endpoint.
	// If probe enabled via VectorAgent, this setting will be ignored for that probe.
	// +optional
	Healthcheck bool `json:"healthcheck,omitempty"`
}

// ConfigCheck is the Schema for control params for ConfigCheck pods
type ConfigCheck struct {
	Disabled bool `json:"disabled,omitempty"`
	// Image - docker image settings for Vector Agent
	// if no specified operator uses default config version
	// +optional
	Image *string `json:"image,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not specified - default setting will be used
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	// +optional
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`
	// Affinity If specified, the pod's scheduling constraints.
	// +optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// Tolerations If specified, the pod's tolerations.
	// +optional
	Tolerations *[]v1.Toleration `json:"tolerations,omitempty"`
	// Annotations is an unstructured key value map stored with a resource that may be set by external tools to store and retrieve arbitrary metadata.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+kubebuilder:printcolumn:name="Valid",type="boolean",JSONPath=".status.configCheckResult"

// Vector is the Schema for the vectors API
type Vector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VectorSpec   `json:"spec,omitempty"`
	Status VectorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VectorList contains a list of Vector
type VectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Vector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Vector{}, &VectorList{})
}
