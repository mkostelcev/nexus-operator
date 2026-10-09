package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RoutingRuleSpec определяет желаемое состояние Routing Rule в Nexus.
type RoutingRuleSpec struct {
	// Имя Routing Rule в Nexus (неизменяемое).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Immutable
	Name string `json:"name"`

	// Описание Routing Rule.
	Description string `json:"description,omitempty"`

	// Режим работы правила маршрутизации.
	// +kubebuilder:validation:Enum=BLOCK;ALLOW
	// +kubebuilder:validation:Required
	Mode string `json:"mode"`

	// Список regex-паттернов для матчинга путей.
	// +kubebuilder:validation:MinItems=1
	Matchers []string `json:"matchers"`
}

// RoutingRuleStatus определяет текущее состояние Routing Rule.
type RoutingRuleStatus struct {
	// Conditions содержит список условий, описывающих состояние ресурса.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Время последней успешной синхронизации.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
	// Количество последовательных ошибок синхронизации.
	// +optional
	SyncErrors int32 `json:"syncErrors,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// RoutingRule — это CRD для управления Routing Rule в Nexus.
type RoutingRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RoutingRuleSpec   `json:"spec,omitempty"`
	Status RoutingRuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RoutingRuleList содержит список Routing Rule.
type RoutingRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RoutingRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RoutingRule{}, &RoutingRuleList{})
}
