package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TeamMember defines a user and their access level.
type TeamMember struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Username string `json:"username"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ro;rw;rwd
	AccessLevel string `json:"accessLevel"`
}

// NexusTeamBindingSpec defines the desired state of NexusTeamBinding.
type NexusTeamBindingSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	TeamAccessRef string `json:"teamAccessRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Members []TeamMember `json:"members"`
}

// NexusTeamBindingStatus defines the observed state of NexusTeamBinding.
type NexusTeamBindingStatus struct {
	Conditions   []metav1.Condition `json:"conditions,omitempty"`
	LastSyncTime *metav1.Time       `json:"lastSyncTime,omitempty"`
	SyncErrors   int32              `json:"syncErrors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="TeamAccess",type="string",JSONPath=".spec.teamAccessRef"
// +kubebuilder:printcolumn:name="Members",type="integer",JSONPath=".spec.members[*]",priority=1
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NexusTeamBinding is the Schema for the nexusteambindings API.
type NexusTeamBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NexusTeamBindingSpec   `json:"spec,omitempty"`
	Status NexusTeamBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NexusTeamBindingList contains a list of NexusTeamBinding.
type NexusTeamBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NexusTeamBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NexusTeamBinding{}, &NexusTeamBindingList{})
}
