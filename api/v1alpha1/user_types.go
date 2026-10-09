package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NexusUserSpec определяет желаемое состояние пользователя Nexus
type NexusUserSpec struct {
	// Уникальный идентификатор пользователя в Nexus (immutable)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9\-_.]+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="userId is immutable"
	UserId string `json:"userId"`

	// Имя пользователя
	// +kubebuilder:validation:Required
	FirstName string `json:"firstName"`

	// Фамилия пользователя
	// +kubebuilder:validation:Required
	LastName string `json:"lastName"`

	// Email адрес пользователя
	// +kubebuilder:validation:Required
	EmailAddress string `json:"emailAddress"`

	// Статус пользователя: active или disabled
	// +kubebuilder:validation:Enum=active;disabled
	// +kubebuilder:default=active
	Status string `json:"status,omitempty"`

	// Список ролей, назначенных пользователю
	// +kubebuilder:validation:MinItems=1
	Roles []string `json:"roles"`

	// Ссылка на Secret с паролем пользователя
	// +kubebuilder:validation:Required
	Credentials CredentialsReference `json:"credentials"`
}

// CredentialsReference содержит ссылку на Secret с паролем
type CredentialsReference struct {
	// Ссылка на ключ в Secret
	SecretKeyRef SecretKeySelector `json:"secretKeyRef"`
}

// SecretKeySelector идентифицирует ключ в Secret
type SecretKeySelector struct {
	// Имя Secret
	Name string `json:"name"`
	// Ключ в Secret
	Key string `json:"key"`
}

// NexusUserStatus определяет текущее состояние пользователя
type NexusUserStatus struct {
	// Условия состояния
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Время последней успешной синхронизации
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Количество последовательных ошибок синхронизации
	// +optional
	SyncErrors int32 `json:"syncErrors,omitempty"`

	// Флаг синхронизации пароля
	// +optional
	CredentialsSynced bool `json:"credentialsSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="UserId",type="string",JSONPath=".spec.userId"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".spec.status"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NexusUser - кастомный ресурс для управления локальными пользователями Nexus
type NexusUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NexusUserSpec   `json:"spec,omitempty"`
	Status NexusUserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NexusUserList содержит список NexusUser
type NexusUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NexusUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NexusUser{}, &NexusUserList{})
}
