package nexus

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"
)

const (
	// Repo Types
	TypeMavenHosted      = "maven-hosted"
	TypeMavenProxy       = "maven-proxy"
	TypeMavenGroup       = "maven-group"
	TypeNpmHosted        = "npm-hosted"
	TypeNpmProxy         = "npm-proxy"
	TypeNpmGroup         = "npm-group"
	TypeDockerHosted     = "docker-hosted"
	TypeDockerProxy      = "docker-proxy"
	TypeDockerGroup      = "docker-group"
	TypeRawHosted        = "raw-hosted"
	TypeRawProxy         = "raw-proxy"
	TypeRawGroup         = "raw-group"
	TypeHelmHosted       = "helm-hosted"
	TypeHelmProxy        = "helm-proxy"
	TypePypiHosted       = "pypi-hosted"
	TypePypiProxy        = "pypi-proxy"
	TypePypiGroup        = "pypi-group"
	TypeNugetHosted      = "nuget-hosted"
	TypeNugetProxy       = "nuget-proxy"
	TypeNugetGroup       = "nuget-group"
	TypeAptHosted        = "apt-hosted"
	TypeAptProxy         = "apt-proxy"
	TypeCargoGroup       = "cargo-group"
	TypeCargoHosted      = "cargo-hosted"
	TypeCargoProxy       = "cargo-proxy"
	TypeCocoapodsProxy   = "cocoapods-proxy"
	TypeComposerProxy    = "composer-proxy"
	TypeConanGroup       = "conan-group"
	TypeConanHosted      = "conan-hosted"
	TypeConanProxy       = "conan-proxy"
	TypeCondaProxy       = "conda-proxy"
	TypeGitlfsHosted     = "gitlfs-hosted"
	TypeGoGroup          = "go-group"
	TypeGoProxy          = "go-proxy"
	TypeHuggingfaceProxy = "huggingface-proxy"
	TypeP2Proxy          = "p2-proxy"
	TypeRGroup           = "r-group"
	TypeRHosted          = "r-hosted"
	TypeRProxy           = "r-proxy"
	TypeRubygemsGroup    = "rubygems-group"
	TypeRubygemsHosted   = "rubygems-hosted"
	TypeRubygemsProxy    = "rubygems-proxy"
	TypeYumGroup         = "yum-group"
	TypeYumHosted        = "yum-hosted"
	TypeYumProxy         = "yum-proxy"
	// Privilege types
	PrivilegeTypeWildcard                  = "wildcard"
	PrivilegeTypeApplication               = "application"
	PrivilegeTypeRepositoryView            = "repository-view"
	PrivilegeTypeRepositoryAdmin           = "repository-admin"
	PrivilegeTypeRepositoryContentSelector = "repository-content-selector"

	RoleAPIPath        = "/service/rest/v1/security/roles"
	UserAPIPath        = "/service/rest/v1/security/users"
	RoutingRuleAPIPath = "/service/rest/v1/routing-rules"
)

// Ошибки для клиента Nexus.
var (
	ErrMissingEnvVars               = errors.New("не заданы необходимые переменные окружения для клиента Nexus")
	ErrUnexpectedResponse           = errors.New("неожиданный статус ответа")
	ErrRepositoryNotFound           = errors.New("репозиторий не найден")
	ErrUnsupportedRepoType          = errors.New("неподдерживаемый тип репозитория")
	ErrContentSelectorNotFound      = errors.New("content-selector не найден")
	ErrContentSelectorAlreadyExists = errors.New("content-selector уже существует")
	ErrPrivilegeNotFound            = errors.New("привелегия не найдена")
	ErrInvalidPrivilegeType         = errors.New("неподдерживаемый тип привелегии")
	ErrWildcardConfigRequired       = errors.New("требуется конфигурация для wildcard-привелегии")
	ErrApplicationConfigRequired    = errors.New("требуется конфигурация для application-привелегии")
	ErrRepoViewConfigRequired       = errors.New("требуется конфигурация для repository-view-привелегии")
	ErrRepoAdminConfigRequired      = errors.New("требуется конфигурация для repository-admin-привелегии")
	ErrRepoContentSelConfigRequired = errors.New("требуется конфигурация для repository-content-selector-привелегии")
	ErrUnsupportedPrivilegeType     = errors.New("неподдерживаемый тип привелегии")
	ErrRoleNotFound                 = errors.New("роль не найдена")
	ErrRoleAlreadyExists            = errors.New("роль уже существует")
	ErrUserNotFound                 = errors.New("пользователь не найден")
	ErrUserAlreadyExists            = errors.New("пользователь уже существует")
	ErrRoutingRuleNotFound          = errors.New("routing rule не найден")

	clientInstance *Client // Глобальный клиент Nexus
	initError      error   // Ошибка инициализации клиента
)

// Client представляет клиент для взаимодействия с Nexus API.
type Client struct {
	Resty  *resty.Client
	Logger *logrus.Logger
}

// initClient инициализирует глобальный клиент Nexus.
func initClient() {
	baseURL := os.Getenv("NEXUS_URL")
	username := os.Getenv("NEXUS_USER")
	password := os.Getenv("NEXUS_PASSWORD")

	if baseURL == "" || username == "" || password == "" {
		initError = fmt.Errorf("%w: baseURL, username или password пусты", ErrMissingEnvVars)
		return
	}

	client, err := NewClient(baseURL, username, password)
	if err != nil {
		initError = err
		return
	}
	clientInstance = client
}

// GetClient возвращает глобальный клиент Nexus.
func GetClient() (*Client, error) {
	if initError != nil {
		return nil, initError
	}

	if clientInstance == nil {
		initClient()
		if initError != nil {
			return nil, initError
		}
	}

	return clientInstance, nil
}

// NewClient создаёт новый экземпляр клиента Nexus.
func NewClient(baseURL, username, password string) (*Client, error) {
	client := resty.New().
		SetBaseURL(baseURL).
		SetBasicAuth(username, password).
		SetTimeout(30 * time.Second)

	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})

	return &Client{
		Resty:  client,
		Logger: logger,
	}, nil
}

// NewUnexpectedResponseError создаёт ошибку с информацией о статусе и тексте ответа.
func NewUnexpectedResponseError(statusCode int, responseText string) error {
	return fmt.Errorf("%w: статус %d, текст: %s", ErrUnexpectedResponse, statusCode, responseText)
}
