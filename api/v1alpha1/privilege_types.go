package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrivilegeSpec определяет желаемое состояние привилегии
type PrivilegeSpec struct {
	// Название привилегии в Nexus (должно быть уникальным)
	Name string `json:"name"`

	// Тип привилегии (обязательное поле)
	// +kubebuilder:validation:Enum=wildcard;application;repository-view;repository-admin;repository-content-selector
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
	// +kubebuilder:validation:Enum=READ;BROWSE;ADD;EDIT;DELETE;RUN;START;STOP;ASSOCIATE;DISASSOCIATE;ALL
	Actions []string `json:"actions"`
}

// RepositoryViewConfig определяет параметры для просмотра репозитория
type RepositoryViewConfig struct {
	// Формат репозитория (maven2, npm, docker и т.д., "*" для всех)
	Format string `json:"format"`

	// Имя репозитория
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// Разрешенные действия
	// +kubebuilder:validation:Enum=READ;BROWSE;ADD;EDIT;DELETE;RUN;START;STOP;ASSOCIATE;DISASSOCIATE;ALL
	Actions []string `json:"actions"`
}

// RepositoryAdminConfig определяет параметры администрирования репозитория
type RepositoryAdminConfig struct {
	// Формат репозитория (maven2, npm, docker и т.д., "*" для всех)
	Format string `json:"format"`

	// Имя репозитория
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// Разрешенные действия
	// +kubebuilder:validation:Enum=READ;BROWSE;ADD;EDIT;DELETE;RUN;START;STOP;ASSOCIATE;DISASSOCIATE;ALL
	Actions []string `json:"actions"`
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
	// +kubebuilder:validation:Items={"type":"string","enum":["READ","BROWSE","ADD","EDIT","DELETE","RUN","START","STOP","ASSOCIATE","DISASSOCIATE","ALL"]}
	Actions []string `json:"actions"`
}

// PrivilegeStatus определяет текущее состояние привилегии
type PrivilegeStatus struct {
	// Условия состояния
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
