package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RepositoryGroup defines a set of repositories of the same format.
type RepositoryGroup struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=docker;maven2;raw;npm;nuget
	Format string `json:"format"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Names []string `json:"names"`

	// +optional
	AccessLevels []string `json:"accessLevels,omitempty"`

	// +optional
	PathPrefix *string `json:"pathPrefix,omitempty"`
}

// NexusTeamAccessSpec defines the desired state of NexusTeamAccess.
type NexusTeamAccessSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9/\-_]+$`
	TeamPath string `json:"teamPath"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Repositories []RepositoryGroup `json:"repositories"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	AccessLevels []string `json:"accessLevels"`
}

// GeneratedResource tracks a child resource created by the controller.
type GeneratedResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	NexusName string `json:"nexusName"`
}

// NexusTeamAccessStatus defines the observed state of NexusTeamAccess.
type NexusTeamAccessStatus struct {
	Conditions         []metav1.Condition  `json:"conditions,omitempty"`
	GeneratedResources []GeneratedResource `json:"generatedResources,omitempty"`
	LastSyncTime       *metav1.Time        `json:"lastSyncTime,omitempty"`
	SyncErrors         int32               `json:"syncErrors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="TeamPath",type="string",JSONPath=".spec.teamPath"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NexusTeamAccess is the Schema for the nexusteamaccesses API.
type NexusTeamAccess struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NexusTeamAccessSpec   `json:"spec,omitempty"`
	Status NexusTeamAccessStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NexusTeamAccessList contains a list of NexusTeamAccess.
type NexusTeamAccessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NexusTeamAccess `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NexusTeamAccess{}, &NexusTeamAccessList{})
}
