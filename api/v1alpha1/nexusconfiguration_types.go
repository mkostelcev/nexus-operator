package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NexusConfigurationSpec определяет желаемое состояние глобальной конфигурации Nexus
type NexusConfigurationSpec struct {
	// Конфигурация HTTP/HTTPS прокси
	// +optional
	HttpProxy *HttpProxySpec `json:"httpProxy,omitempty"`

	// Конфигурация активных Security Realm'ов
	// +optional
	SecurityRealms *SecurityRealmsSpec `json:"securityRealms,omitempty"`
}

// HttpProxySpec определяет настройки HTTP/HTTPS прокси для Nexus
type HttpProxySpec struct {
	// Настройки HTTP прокси
	// +optional
	HttpProxy *HttpProxyServerConfig `json:"httpProxy,omitempty"`

	// Настройки HTTPS прокси
	// +optional
	HttpsProxy *HttpProxyServerConfig `json:"httpsProxy,omitempty"`

	// Список хостов, для которых прокси не используется
	// +optional
	NonProxyHosts []string `json:"nonProxyHosts,omitempty"`
}

// HttpProxyServerConfig определяет параметры одного прокси-сервера
type HttpProxyServerConfig struct {
	// Включён ли прокси
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// Хост прокси-сервера
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// Порт прокси-сервера (1-65535)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[0-9]+$`
	Port string `json:"port"`

	// Параметры аутентификации (опционально)
	// +optional
	AuthInfo *ProxyAuthConfig `json:"authInfo,omitempty"`
}

// ProxyAuthConfig определяет параметры аутентификации прокси
type ProxyAuthConfig struct {
	// Включена ли аутентификация
	Enabled bool `json:"enabled"`

	// Имя пользователя
	// +optional
	Username string `json:"username,omitempty"`

	// Пароль
	// +optional
	Password string `json:"password,omitempty"`

	// NTLM хост (опционально)
	// +optional
	NtlmHost string `json:"ntlmHost,omitempty"`

	// NTLM домен (опционально)
	// +optional
	NtlmDomain string `json:"ntlmDomain,omitempty"`
}

// SecurityRealmsSpec определяет активные Security Realm'ы Nexus
type SecurityRealmsSpec struct {
	// Список ID активных realm'ов в порядке приоритета
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Active []string `json:"active"`
}

// NexusConfigurationStatus определяет текущее состояние NexusConfiguration
type NexusConfigurationStatus struct {
	// Условия состояния
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Время последней успешной синхронизации
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Количество последовательных ошибок синхронизации
	// +optional
	SyncErrors int32 `json:"syncErrors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NexusConfiguration — глобальная конфигурация Nexus (HTTP Proxy + Security Realms)
type NexusConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NexusConfigurationSpec   `json:"spec,omitempty"`
	Status NexusConfigurationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NexusConfigurationList содержит список NexusConfiguration
type NexusConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NexusConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NexusConfiguration{}, &NexusConfigurationList{})
}
