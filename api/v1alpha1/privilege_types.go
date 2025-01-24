package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrivilegeSpec определяет желаемое состояние привилегии
type PrivilegeSpec struct {
	// Название привилегии в Nexus (должно быть уникальным)
	Name string `json:"name"`

	// Тип привилегии (обязательное поле)
	// +kubebuilder:validation:Enum=wildcard;application;repository-view;repository-admin;repository-content-selector;script
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Описание привилегии (необязательное)
	Description string `json:"description,omitempty"`

	// Конфигурация для типа wildcard
	Wildcard *WildcardConfig `json:"wildcard,omitempty"`

	// Конфигурация для типа application
	Application *ApplicationConfig `json:"application,omitempty"`

	// Конфигурация для типа repository-view
	RepositoryView *RepositoryViewConfig `json:"repositoryView,omitempty"`

	// Конфигурация для типа repository-admin
	RepositoryAdmin *RepositoryAdminConfig `json:"repositoryAdmin,omitempty"`

	// Конфигурация для типа repository-content-selector
	RepositoryContentSelector *RepositoryContentSelectorConfig `json:"repositoryContentSelector,omitempty"`

	// Конфигурация для типа script
	Script *ScriptConfig `json:"script,omitempty"`
}

// WildcardConfig определяет параметры для wildcard-привилегии
type WildcardConfig struct {
	// Паттерн для wildcard-доступа (пример: "nexus:*")
	// +kubebuilder:validation:Required
	Pattern string `json:"pattern"`
}

// ApplicationConfig определяет параметры для привилегии приложения
type ApplicationConfig struct {
	// Домен приложения
	// +kubebuilder:validation:Required
	Domain string `json:"domain"`

	// Разрешенные действия
	// +kubebuilder:validation:Enum=READ;BROWSE;ADD;EDIT;DELETE;RUN;ASSOCIATE;DISASSOCIATE;ALL
	Actions []string `json:"actions"`
}

// RepositoryViewConfig определяет параметры для просмотра репозитория
type RepositoryViewConfig struct {
	// Имя репозитория
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// Разрешенные действия
	// +kubebuilder:validation:Enum=READ;BROWSE;ADD;EDIT;DELETE;RUN;ASSOCIATE;DISASSOCIATE;ALL
	Actions []string `json:"actions"`
}

// RepositoryAdminConfig определяет параметры администрирования репозитория
type RepositoryAdminConfig struct {
	// Имя репозитория
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`
}

// RepositoryContentSelectorConfig определяет параметры селектора контента
type RepositoryContentSelectorConfig struct {
	// Имя репозитория
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// Имя content-selector'а
	// +kubebuilder:validation:Required
	ContentSelector string `json:"contentSelector"`

	// Формат репозитория (maven2, npm, docker и т.д.)
	// +kubebuilder:validation:Required
	Format string `json:"format"`

	// Разрешенные действия
	// +kubebuilder:validation:Type=array
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:Items={"type":"string","enum":["READ","BROWSE","ADD","EDIT","DELETE","RUN","ASSOCIATE","DISASSOCIATE","ALL"]}
	Actions []string `json:"actions"`
}

// ScriptConfig определяет параметры для скриптовых привилегий
type ScriptConfig struct {
	// Имя скрипта
	// +kubebuilder:validation:Required
	ScriptName string `json:"scriptName"`
}

// PrivilegeStatus определяет текущее состояние привилегии
type PrivilegeStatus struct {
	// Условия состояния
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Privilege - кастомный ресурс для управления привилегиями Nexus
type Privilege struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PrivilegeSpec   `json:"spec,omitempty"`
	Status PrivilegeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PrivilegeList содержит список Privilege
type PrivilegeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Privilege `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Privilege{}, &PrivilegeList{})
}
