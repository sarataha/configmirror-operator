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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ConfigMirrorSpec defines the desired state of ConfigMirror
type ConfigMirrorSpec struct {
	// SourceNamespace is the namespace to watch for ConfigMaps
	// +kubebuilder:validation:Required
	SourceNamespace string `json:"sourceNamespace"`

	// TargetNamespaces is a list of namespaces to replicate ConfigMaps to
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	TargetNamespaces []string `json:"targetNamespaces"`

	// Selector is a label selector for ConfigMaps to mirror
	// +kubebuilder:validation:Required
	Selector *metav1.LabelSelector `json:"selector"`

	// Database configuration for storing ConfigMap data
	// +optional
	Database *DatabaseConfig `json:"database,omitempty"`
}

// DatabaseConfig specifies PostgreSQL connection configuration
type DatabaseConfig struct {
	// Enabled determines if database storage is enabled
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// SecretRef references a Secret containing database connection details
	// Expected keys: host, port, dbname, username, password
	// +kubebuilder:validation:Required
	SecretRef SecretReference `json:"secretRef"`
}

// SecretReference contains information to locate a Secret
type SecretReference struct {
	// Name of the Secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the Secret (defaults to ConfigMirror namespace if empty)
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ConfigMirrorStatus defines the observed state of ConfigMirror.
type ConfigMirrorStatus struct {
	// Conditions represent the current state of the ConfigMirror resource
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ReplicatedConfigMaps contains information about replicated ConfigMaps
	// +optional
	ReplicatedConfigMaps []ReplicatedConfigMap `json:"replicatedConfigMaps,omitempty"`

	// DatabaseStatus contains information about database connection
	// +optional
	DatabaseStatus *DatabaseStatus `json:"databaseStatus,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed ConfigMirror
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// ReplicatedConfigMap contains status for a single replicated ConfigMap
type ReplicatedConfigMap struct {
	// Name of the ConfigMap
	Name string `json:"name"`

	// SourceNamespace is the namespace the ConfigMap was replicated from
	SourceNamespace string `json:"sourceNamespace"`

	// Targets is a list of namespaces the ConfigMap was replicated to
	Targets []string `json:"targets"`

	// LastSyncTime is the last time the ConfigMap was successfully synced
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
}

// DatabaseStatus contains database connection status
type DatabaseStatus struct {
	// Connected indicates if the database connection is healthy
	Connected bool `json:"connected"`

	// LastSyncTime is the last time data was successfully written to the database
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Message contains additional status information
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cm
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ConfigMaps",type="integer",JSONPath=".status.replicatedConfigMaps[*].name",description="Number of replicated ConfigMaps"
// +kubebuilder:printcolumn:name="DB",type="string",JSONPath=".status.databaseStatus.connected",description="Database connected"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ConfigMirror is the Schema for the configmirrors API
type ConfigMirror struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ConfigMirror
	// +required
	Spec ConfigMirrorSpec `json:"spec"`

	// status defines the observed state of ConfigMirror
	// +optional
	Status ConfigMirrorStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ConfigMirrorList contains a list of ConfigMirror
type ConfigMirrorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigMirror `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigMirror{}, &ConfigMirrorList{})
}
