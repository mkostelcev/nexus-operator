package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RoleSpec определяет желаемое состояние роли
type RoleSpec struct {
	// Уникальный идентификатор роли (должен соответствовать формату Nexus)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9\-_]+$`
	RoleID string `json:"roleId"`

	// Человекочитаемое имя роли
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Описание роли
	// +kubebuilder:validation:MaxLength=255
	Description string `json:"description,omitempty"`

	// Список привилегий, назначаемых роли
	// +kubebuilder:validation:MinItems=0
	Privileges []string `json:"privileges,omitempty"`

	// Список дочерних ролей
	// +kubebuilder:validation:MinItems=0
	Roles []string `json:"roles,omitempty"`

	// Конфигурация внешних источников ролей (опционально)
	Source *RoleSource `json:"source,omitempty"`
}

// RoleSource определяет внешний источник для роли
type RoleSource struct {
	// Тип источника (например: ldap, saml)
	// +kubebuilder:validation:Enum=ldap;saml;crowd
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Имя источника в конфигурации Nexus
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// RoleStatus определяет текущее состояние роли
type RoleStatus struct {
	// Условия состояния
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Сообщение о текущем статусе
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="RoleID",type="string",JSONPath=".spec.roleId"
// +kubebuilder:printcolumn:name="Exists",type="boolean",JSONPath=".status.exists"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Role - кастомный ресурс для управления ролями Nexus
type Role struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RoleSpec   `json:"spec,omitempty"`
	Status RoleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RoleList содержит список Role
type RoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Role `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Role{}, &RoleList{})
}
