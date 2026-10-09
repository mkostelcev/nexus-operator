package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RepositorySpec определяет желаемое состояние репозитория Nexus.
type RepositorySpec struct {
	// Name - уникальное имя репозитория (неизменяемое).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Immutable
	Name string `json:"name"`

	// Type - тип репозитория (например, maven-hosted, npm-hosted и т.д.) (неизменяемое).
	// +kubebuilder:validation:Enum=maven-hosted;maven-proxy;maven-group;npm-hosted;npm-proxy;npm-group;docker-hosted;docker-group;docker-proxy;raw-hosted;raw-group;raw-proxy;helm-hosted;helm-proxy;pypi-hosted;pypi-proxy;pypi-group;nuget-hosted;nuget-proxy;nuget-group;apt-hosted;apt-proxy;cargo-group;cargo-hosted;cargo-proxy;cocoapods-proxy;composer-proxy;conan-group;conan-hosted;conan-proxy;conda-proxy;gitlfs-hosted;go-group;go-proxy;huggingface-proxy;p2-proxy;r-group;r-hosted;r-proxy;rubygems-group;rubygems-hosted;rubygems-proxy;yum-group;yum-hosted;yum-proxy
	// +kubebuilder:validation:Immutable
	Type string `json:"type"`

	// Online указывает, доступен ли репозиторий.
	// +kubebuilder:default=true
	Online bool `json:"online"`

	// Storage содержит детали конфигурации хранения.
	Storage StorageConfig `json:"storage"`

	// Maven - настройки для Maven (опционально).
	// +optional
	Maven *MavenConfig `json:"maven,omitempty"`

	// NPM - настройки для NPM (опционально).
	// +optional
	Npm *NpmConfig `json:"npm,omitempty"`

	// Docker - настройки для Docker (опционально).
	// +optional
	Docker *DockerConfig `json:"docker,omitempty"`

	// Raw - настройки для Raw (опционально).
	// +optional
	Raw *RawConfig `json:"raw,omitempty"`

	// Proxy - настройки для прокси (опционально).
	// +optional
	Proxy *ProxyConfig `json:"proxy,omitempty"`

	// Group - настройки для групп (опционально).
	// +optional
	Group *GroupConfig `json:"group,omitempty"`

	// Cleanup - настройки политики очистки (опционально).
	// +optional
	Cleanup *CleanupPolicy `json:"cleanup,omitempty"`

	// HttpClient содержит настройки HTTP-клиента.
	// +optional
	HttpClient *HttpClientConfig `json:"httpClient,omitempty"`

	// NegativeCache содержит настройки отрицательного кэша.
	// +optional
	NegativeCache *NegativeCacheConfig `json:"negativeCache,omitempty"`

	// Apt - настройки для APT (опционально).
	// +optional
	Apt *AptConfig `json:"apt,omitempty"`

	// AptSigning - настройки подписи для APT hosted (опционально).
	// +optional
	AptSigning *AptSigningConfig `json:"aptSigning,omitempty"`

	// Yum - настройки для YUM hosted (опционально).
	// +optional
	Yum *YumConfig `json:"yum,omitempty"`

	// YumSigning - настройки подписи для YUM (опционально).
	// +optional
	YumSigning *YumSigningConfig `json:"yumSigning,omitempty"`

	// NugetProxy - настройки для NuGet proxy (опционально).
	// +optional
	NugetProxy *NugetProxyConfig `json:"nugetProxy,omitempty"`

	// Cargo - настройки для Cargo (опционально).
	// +optional
	Cargo *CargoConfig `json:"cargo,omitempty"`

	// ConanProxy - настройки для Conan proxy (опционально).
	// +optional
	ConanProxy *ConanProxyConfig `json:"conanProxy,omitempty"`

	// RoutingRuleName - имя Routing Rule, привязанного к репозиторию (опционально).
	// +optional
	RoutingRuleName string `json:"routingRuleName,omitempty"`
}

// RepositoryStatus описывает состояние репозитория.
type RepositoryStatus struct {
	// Conditions содержит список условий, описывающих состояние ресурса.
	// +optional
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	// Время последней успешной синхронизации.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
	// Количество последовательных ошибок синхронизации.
	// +optional
	SyncErrors int32 `json:"syncErrors,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Repository - это схема для API репозиториев.
type Repository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RepositorySpec   `json:"spec,omitempty"`
	Status RepositoryStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RepositoryList содержит список объектов Repository.
type RepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Repository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Repository{}, &RepositoryList{})
}

// HttpClientConfig описывает настройки HTTP-клиента.
type HttpClientConfig struct {
	// Blocked указывает, блокируется ли доступ к удалённому репозиторию.
	// +kubebuilder:default=false
	Blocked bool `json:"blocked"`

	// AutoBlock включает автоматическую блокировку при недоступности репозитория.
	// +kubebuilder:default=true
	AutoBlock bool `json:"autoBlock"`

	// Connection содержит настройки HTTP-соединения.
	// +optional
	Connection *ConnectionConfig `json:"connection,omitempty"`

	// Authentication содержит настройки аутентификации.
	// +optional
	Authentication *AuthConfig `json:"authentication,omitempty"`
}

// ConnectionConfig описывает настройки HTTP-соединения для proxy-репозиториев.
type ConnectionConfig struct {
	// Retries — количество повторных попыток при таймауте (0-10).
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	Retries *int `json:"retries,omitempty"`

	// Timeout — таймаут ожидания активности в секундах (1-3600).
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	Timeout *int `json:"timeout,omitempty"`

	// UserAgentSuffix — суффикс для заголовка User-Agent.
	// +optional
	UserAgentSuffix string `json:"userAgentSuffix,omitempty"`

	// EnableCircularRedirects разрешает редиректы на тот же URL.
	// +optional
	EnableCircularRedirects bool `json:"enableCircularRedirects,omitempty"`

	// EnableCookies разрешает сохранение и использование cookies.
	// +optional
	EnableCookies bool `json:"enableCookies,omitempty"`

	// UseTrustStore использует сертификаты из Nexus truststore.
	// +optional
	UseTrustStore bool `json:"useTrustStore,omitempty"`
}

// NegativeCacheConfig описывает настройки отрицательного кэша.
type NegativeCacheConfig struct {
	// Enabled включает отрицательное кэширование.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// TimeToLive задаёт время жизни отрицательного кэша в секундах.
	// +optional
	// +kubebuilder:default=300
	TimeToLive int `json:"timeToLive,omitempty"`
}

// StorageConfig определяет настройки, связанные с хранением для репозитория.
type StorageConfig struct {
	// BlobStoreName указывает имя хранилища блобов(неизменяемое).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Immutable
	BlobStoreName string `json:"blobStoreName"`

	// StrictContentTypeValidation указывает, будет ли применяться строгая проверка типа содержимого.
	// +kubebuilder:default=true
	StrictContentTypeValidation bool `json:"strictContentTypeValidation"`

	// WritePolicy определяет политику записи для репозитория.
	// +kubebuilder:validation:Enum=ALLOW_ONCE;ALLOW;DENY
	// +kubebuilder:default=ALLOW
	WritePolicy string `json:"writePolicy"`
}

// MavenConfig определяет настройки, специфичные для Maven.
type MavenConfig struct {
	// VersionPolicy определяет политику версий (например, RELEASE, SNAPSHOT, MIXED).
	// +kubebuilder:validation:Enum=RELEASE;SNAPSHOT;MIXED
	VersionPolicy string `json:"versionPolicy"`

	// LayoutPolicy определяет политику макета (например, STRICT, PERMISSIVE).
	// +kubebuilder:validation:Enum=STRICT;PERMISSIVE
	LayoutPolicy string `json:"layoutPolicy"`

	// ContentDisposition для совместимости с групповыми репозиториями
	// +kubebuilder:validation:Enum=INLINE;ATTACHMENT
	// +kubebuilder:default=INLINE
	ContentDisposition string `json:"contentDisposition,omitempty"`
}

// NpmConfig определяет настройки, специфичные для NPM.
type NpmConfig struct {
	// RemoveNonCataloged определяет, нужно ли удалять некаталогизированные компоненты.
	RemoveNonCataloged bool `json:"removeNonCataloged,omitempty"`

	// RemoveQuarantined определяет, нужно ли удалять компоненты из карантина.
	RemoveQuarantined bool `json:"removeQuarantined,omitempty"`
}

// DockerConfig определяет настройки, специфичные для Docker.
type DockerConfig struct {
	// HttpPort указывает HTTP порт для Docker репозитория.
	HttpPort *int `json:"httpPort,omitempty"`

	// HttpsPort указывает HTTPS порт для Docker репозитория.
	HttpsPort *int `json:"httpsPort,omitempty"`

	// ForceBasicAuth определяет, будет ли применяться базовая аутентификация.
	ForceBasicAuth bool `json:"forceBasicAuth,omitempty"`

	// V1Enabled указывает, включен ли API Docker V1.
	// +kubebuilder:default=false
	V1Enabled bool `json:"v1Enabled"`

	// Subdomain указывает поддомен для репозитория.
	Subdomain string `json:"subdomain,omitempty"`
}

// RawConfig определяет настройки, специфичные для Raw.
type RawConfig struct {
	// ContentDisposition определяет поведение при скачивании файлов.
	// +kubebuilder:validation:Enum=INLINE;ATTACHMENT
	// +kubebuilder:default=INLINE
	ContentDisposition string `json:"contentDisposition,omitempty"`
}

// ProxyConfig определяет настройки прокси для репозитория.
type ProxyConfig struct {
	// RemoteUrl указывает удалённый URL для прокси-репозитория.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(http|https)://.+`
	RemoteUrl string `json:"remoteUrl"`

	// ContentMaxAge определяет максимальный возраст кэшированного контента.
	ContentMaxAge int `json:"contentMaxAge,omitempty"`

	// MetadataMaxAge определяет максимальный возраст кэшированных метаданных.
	MetadataMaxAge int `json:"metadataMaxAge,omitempty"`
}

// AuthConfig описывает параметры HTTP-аутентификации.
type AuthConfig struct {
	// Type - тип авторизации (Username/BasicAuth, NTLM или bearerToken)
	// +kubebuilder:validation:Enum=username;ntlm;bearerToken
	// +kubebuilder:default=username
	Type string `json:"type,omitempty"`

	// Username - имя пользователя для базовой аутентификации.
	// +optional
	Username string `json:"username,omitempty"`

	// Password - пароль для базовой аутентификации (inline, не рекомендуется).
	// +optional
	Password string `json:"password,omitempty"`

	// SecretRef - ссылка на ключ в Secret с JSON-объектом {"username":"...","password":"..."}.
	// Имеет приоритет над inline-полями Username и Password.
	// +optional
	SecretRef *SecretKeySelector `json:"secretRef,omitempty"`

	// BearerToken - токен для Bearer-аутентификации.
	// +optional
	BearerToken string `json:"bearerToken,omitempty"`
}

// GroupConfig определяет настройки группы для репозитория.
type GroupConfig struct {
	// MemberNames содержит список участников группового репозитория.
	// +kubebuilder:validation:Required
	MemberNames []string `json:"memberNames"`
}

// CleanupPolicy определяет политики очистки для репозитория.
type CleanupPolicy struct {
	// PolicyNames содержит список политик очистки, применяемых к репозиторию.
	PolicyNames []string `json:"policyNames,omitempty"`
}

// AptConfig определяет настройки, специфичные для APT.
type AptConfig struct {
	// Distribution — дистрибутив для APT.
	// +optional
	Distribution string `json:"distribution,omitempty"`

	// Flat указывает, является ли репозиторий плоским (только для proxy).
	// +optional
	Flat bool `json:"flat"`
}

// AptSigningConfig определяет настройки подписи для APT hosted.
type AptSigningConfig struct {
	// Keypair — PGP-ключ для подписи (armored private key).
	// +optional
	Keypair string `json:"keypair,omitempty"`

	// Passphrase — пароль для PGP-ключа.
	// +optional
	Passphrase string `json:"passphrase,omitempty"`
}

// YumConfig определяет настройки, специфичные для YUM hosted.
type YumConfig struct {
	// RepodataDepth — глубина каталога для repodata.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=5
	RepodataDepth int `json:"repodataDepth"`

	// DeployPolicy — политика деплоя RPM-пакетов.
	// +kubebuilder:validation:Enum=PERMISSIVE;STRICT
	// +optional
	DeployPolicy string `json:"deployPolicy,omitempty"`
}

// YumSigningConfig определяет настройки подписи для YUM.
type YumSigningConfig struct {
	// Keypair — PGP-ключ для подписи (armored private key).
	// +optional
	Keypair string `json:"keypair,omitempty"`

	// Passphrase — пароль для PGP-ключа.
	// +optional
	Passphrase string `json:"passphrase,omitempty"`
}

// NugetProxyConfig определяет настройки, специфичные для NuGet proxy.
type NugetProxyConfig struct {
	// QueryCacheItemMaxAge — время кэширования результатов запросов (в секундах).
	// +optional
	QueryCacheItemMaxAge int `json:"queryCacheItemMaxAge,omitempty"`

	// NugetVersion — версия протокола NuGet.
	// +kubebuilder:validation:Enum=V2;V3
	// +optional
	NugetVersion string `json:"nugetVersion,omitempty"`
}

// CargoConfig определяет настройки, специфичные для Cargo.
type CargoConfig struct {
	// RequireAuthentication указывает, требует ли репозиторий аутентификации.
	// +optional
	RequireAuthentication bool `json:"requireAuthentication,omitempty"`
}

// ConanProxyConfig определяет настройки, специфичные для Conan proxy.
type ConanProxyConfig struct {
	// ConanVersion — версия протокола Conan.
	// +kubebuilder:validation:Enum=V1;V2
	// +optional
	ConanVersion string `json:"conanVersion,omitempty"`
}
