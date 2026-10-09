// Работа с репозиториями в Sonatype Nexus
package nexus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// repoTypeToAPIPath конвертирует тип оператора (например, "maven-hosted") в базовый путь Nexus REST API.
// Тип всегда имеет формат "{format}-{kind}", например "apt-hosted" → "/service/rest/v1/repositories/apt/hosted".
func repoTypeToAPIPath(repoType string) (string, error) {
	parts := strings.SplitN(repoType, "-", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedRepoType, repoType)
	}
	return fmt.Sprintf("/service/rest/v1/repositories/%s/%s", parts[0], parts[1]), nil
}

// CreateRepository создаёт репозиторий указанного типа.
func (c *Client) CreateRepository(ctx context.Context, repoType string, config map[string]interface{}) error {
	endpoint, err := repoTypeToAPIPath(repoType)
	if err != nil {
		return err
	}

	c.Logger.Infof("Создание репозитория типа %s", repoType)
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(config).
		SetHeader("Content-Type", "application/json").
		Post(endpoint)
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() != 201 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return nil
}

// UpdateRepository обновляет существующий репозиторий.
func (c *Client) UpdateRepository(ctx context.Context, repoType, name string, config map[string]interface{}) error {
	basePath, err := repoTypeToAPIPath(repoType)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/%s", basePath, name)

	c.Logger.Infof("Обновление репозитория типа %s", repoType)
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(config).
		SetHeader("Content-Type", "application/json").
		Put(endpoint)
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	// Считаем успешными ответы 200 и 204
	if resp.StatusCode() != 200 && resp.StatusCode() != 204 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return nil
}

// GetRepository получает конфигурацию существующего репозитория.
func (c *Client) GetRepository(ctx context.Context, name string) (map[string]interface{}, error) {
	c.Logger.Infof("Получение конфигурации репозитория: %s", name)
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Get("/service/rest/v1/repositories/{name}")
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса: %w", err)
	}

	if resp.StatusCode() == 404 {
		return nil, ErrRepositoryNotFound
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("ошибка разбора ответа: %w", err)
	}

	return result, nil
}

// DeleteRepository удаляет репозиторий из Nexus.
func (c *Client) DeleteRepository(ctx context.Context, name string) error {
	c.Logger.Infof("Удаление репозитория: %s", name)
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Delete("/service/rest/v1/repositories/{name}")
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() == 404 {
		return ErrRepositoryNotFound
	}

	if resp.StatusCode() != 204 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	c.Logger.Infof("Репозиторий удалён: %s", name)
	return nil
}

// BuildRepositoryConfig создаёт конфигурацию для репозитория указанного типа.
func BuildRepositoryConfig(repo v1alpha1.Repository) (map[string]interface{}, error) {
	config := map[string]interface{}{
		"name":    repo.Spec.Name,
		"online":  repo.Spec.Online,
		"storage": repo.Spec.Storage,
	}

	// Общая обработка proxy-конфигурации
	if repo.Spec.Proxy != nil {
		config["proxy"] = buildProxyConfig(repo.Spec.Proxy)
	}

	// Обработка групп
	if repo.Spec.Group != nil {
		config["group"] = repo.Spec.Group
	}

	// Общая обработка httpClient и negativeCache
	if repo.Spec.HttpClient != nil {
		config["httpClient"] = buildHttpClientConfig(repo.Spec.HttpClient)
	}
	if repo.Spec.NegativeCache != nil {
		config["negativeCache"] = repo.Spec.NegativeCache
	}
	if repo.Spec.Cleanup != nil {
		config["cleanup"] = repo.Spec.Cleanup
	}
	if repo.Spec.RoutingRuleName != "" {
		config["routingRule"] = repo.Spec.RoutingRuleName
	}

	// Тип-специфичная конфигурация
	if err := applyTypeSpecificConfig(config, repo); err != nil {
		return nil, err
	}

	return config, nil
}

// applyTypeSpecificConfig применяет конфигурацию, специфичную для типа репозитория.
func applyTypeSpecificConfig(
	config map[string]interface{}, repo v1alpha1.Repository,
) error {
	switch repo.Spec.Type {
	case TypeMavenProxy, TypeMavenHosted:
		config["maven"] = repo.Spec.Maven
	case TypeMavenGroup:
		if repo.Spec.Maven != nil {
			config["maven"] = repo.Spec.Maven
		}
	case TypeNpmHosted, TypeNpmProxy:
		config["npm"] = repo.Spec.Npm
	case TypeNpmGroup:
		applyNpmGroupConfig(config, repo)
	case TypeDockerProxy:
		applyDockerProxyConfig(config, repo)
	case TypeDockerGroup:
		applyDockerGroupConfig(config, repo)
	case TypeDockerHosted:
		applyDockerHostedConfig(config, repo)
	case TypeRawHosted:
		config["storage"] = map[string]interface{}{
			"blobStoreName":               repo.Spec.Storage.BlobStoreName,
			"strictContentTypeValidation": repo.Spec.Storage.StrictContentTypeValidation,
			"writePolicy":                 repo.Spec.Storage.WritePolicy,
		}
	case TypeRawProxy:
		config["proxy"] = map[string]interface{}{
			"remoteUrl":      repo.Spec.Proxy.RemoteUrl,
			"contentMaxAge":  repo.Spec.Proxy.ContentMaxAge,
			"metadataMaxAge": repo.Spec.Proxy.MetadataMaxAge,
		}
		if repo.Spec.HttpClient != nil {
			config["httpClient"] = buildHttpClientConfig(repo.Spec.HttpClient)
		}
	case TypeAptHosted:
		applyAptHostedConfig(config, repo)
	case TypeAptProxy:
		applyAptProxyConfig(config, repo)
	case TypeYumHosted:
		if repo.Spec.Yum != nil {
			config["yum"] = repo.Spec.Yum
		}
	case TypeYumProxy:
		if repo.Spec.YumSigning != nil {
			config["yumSigning"] = repo.Spec.YumSigning
		}
	case TypeYumGroup:
		applyYumGroupConfig(config, repo)
	case TypeNugetProxy:
		applyNugetProxyConfig(config, repo)
	case TypeCargoProxy:
		if repo.Spec.Cargo != nil {
			config["cargo"] = repo.Spec.Cargo
		}
	case TypeCargoGroup:
		applyCargoGroupConfig(config, repo)
	case TypeConanProxy:
		if repo.Spec.ConanProxy != nil {
			config["conanProxy"] = repo.Spec.ConanProxy
		}
	// Hosted-типы без format-специфичной конфигурации
	case TypeHelmHosted, TypePypiHosted, TypeNugetHosted, TypeCargoHosted,
		TypeConanHosted, TypeGitlfsHosted, TypeRHosted, TypeRubygemsHosted:
	// Proxy-типы со стандартной proxy-конфигурацией
	case TypeHelmProxy, TypePypiProxy, TypeCocoapodsProxy, TypeComposerProxy,
		TypeCondaProxy, TypeGoProxy, TypeHuggingfaceProxy, TypeP2Proxy,
		TypeRProxy, TypeRubygemsProxy:
	// Group-типы с memberNames
	case TypeRawGroup, TypePypiGroup, TypeNugetGroup, TypeConanGroup,
		TypeGoGroup, TypeRGroup, TypeRubygemsGroup:
		config["group"] = map[string]interface{}{
			"memberNames": repo.Spec.Group.MemberNames,
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedRepoType, repo.Spec.Type)
	}

	return nil
}

// applyNpmGroupConfig применяет конфигурацию для npm-group.
func applyNpmGroupConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	config["storage"] = map[string]interface{}{
		"blobStoreName":               repo.Spec.Storage.BlobStoreName,
		"strictContentTypeValidation": repo.Spec.Storage.StrictContentTypeValidation,
		"writePolicy":                 repo.Spec.Storage.WritePolicy,
	}
	config["group"] = repo.Spec.Group
	if repo.Spec.Npm != nil {
		config["npm"] = map[string]interface{}{
			"removeNonCataloged": repo.Spec.Npm.RemoveNonCataloged,
			"removeQuarantined":  repo.Spec.Npm.RemoveQuarantined,
		}
	}
}

// applyDockerProxyConfig применяет конфигурацию для docker-proxy.
func applyDockerProxyConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	config["docker"] = map[string]interface{}{
		"httpPort":       repo.Spec.Docker.HttpPort,
		"httpsPort":      repo.Spec.Docker.HttpsPort,
		"forceBasicAuth": repo.Spec.Docker.ForceBasicAuth,
		"v1Enabled":      repo.Spec.Docker.V1Enabled,
		"subdomain":      repo.Spec.Docker.Subdomain,
	}
	// remoteUrl и сроки кэша уходят в общий блок proxy. В dockerProxy Nexus их не хранит и в GET не отдаёт,
	// поэтому их копия здесь давала вечный diff и лишний PUT на каждом reconcile
	config["dockerProxy"] = map[string]interface{}{
		"indexType": "REGISTRY",
	}
	if repo.Spec.HttpClient != nil {
		config["httpClient"] = buildHttpClientConfig(repo.Spec.HttpClient)
	}
}

// applyDockerGroupConfig применяет конфигурацию для docker-group.
func applyDockerGroupConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	dockerGroupConfig := map[string]interface{}{
		"forceBasicAuth": false,
		"v1Enabled":      false,
	}
	if repo.Spec.Docker != nil {
		dockerGroupConfig["forceBasicAuth"] = repo.Spec.Docker.ForceBasicAuth
		dockerGroupConfig["v1Enabled"] = repo.Spec.Docker.V1Enabled
		if repo.Spec.Docker.Subdomain != "" {
			dockerGroupConfig["subdomain"] = repo.Spec.Docker.Subdomain
		}
		if repo.Spec.Docker.HttpPort != nil {
			dockerGroupConfig["httpPort"] = *repo.Spec.Docker.HttpPort
		}
		if repo.Spec.Docker.HttpsPort != nil {
			dockerGroupConfig["httpsPort"] = *repo.Spec.Docker.HttpsPort
		}
	}
	config["docker"] = dockerGroupConfig
	config["group"] = repo.Spec.Group
}

// applyDockerHostedConfig применяет конфигурацию для docker-hosted.
func applyDockerHostedConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	dockerConfig := map[string]interface{}{
		"forceBasicAuth": repo.Spec.Docker.ForceBasicAuth,
		"v1Enabled":      repo.Spec.Docker.V1Enabled,
		"subdomain":      repo.Spec.Docker.Subdomain,
	}
	if repo.Spec.Docker.HttpPort != nil {
		dockerConfig["httpPort"] = *repo.Spec.Docker.HttpPort
	}
	if repo.Spec.Docker.HttpsPort != nil {
		dockerConfig["httpsPort"] = *repo.Spec.Docker.HttpsPort
	}
	config["docker"] = dockerConfig
}

// applyAptHostedConfig применяет конфигурацию для apt-hosted.
func applyAptHostedConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	if repo.Spec.Apt != nil {
		config["apt"] = repo.Spec.Apt
	} else {
		config["apt"] = map[string]interface{}{}
	}
	if repo.Spec.AptSigning != nil {
		config["aptSigning"] = repo.Spec.AptSigning
	} else {
		config["aptSigning"] = map[string]interface{}{}
	}
}

// applyAptProxyConfig применяет конфигурацию для apt-proxy.
func applyAptProxyConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	if repo.Spec.Apt != nil {
		config["apt"] = repo.Spec.Apt
	} else {
		config["apt"] = map[string]interface{}{}
	}
}

// applyYumGroupConfig применяет конфигурацию для yum-group.
func applyYumGroupConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	config["group"] = map[string]interface{}{
		"memberNames": repo.Spec.Group.MemberNames,
	}
	if repo.Spec.YumSigning != nil {
		config["yumSigning"] = repo.Spec.YumSigning
	}
}

// applyNugetProxyConfig применяет конфигурацию для nuget-proxy.
func applyNugetProxyConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	if repo.Spec.NugetProxy != nil {
		config["nugetProxy"] = repo.Spec.NugetProxy
	} else {
		config["nugetProxy"] = map[string]interface{}{}
	}
}

// applyCargoGroupConfig применяет конфигурацию для cargo-group.
func applyCargoGroupConfig(config map[string]interface{}, repo v1alpha1.Repository) {
	config["group"] = map[string]interface{}{
		"memberNames": repo.Spec.Group.MemberNames,
	}
	if repo.Spec.Cargo != nil {
		config["cargo"] = repo.Spec.Cargo
	}
}

// buildHttpClientConfig создаёт конфигурацию httpClient с явными дефолтами для connection.
// Это гарантирует, что ручные изменения connection в Nexus UI будут сброшены к desired state.
func buildHttpClientConfig(hc *v1alpha1.HttpClientConfig) map[string]interface{} {
	result := map[string]interface{}{
		"blocked":   hc.Blocked,
		"autoBlock": hc.AutoBlock,
	}

	// Всегда включаем connection с дефолтами Nexus,
	// чтобы оператор мог сбросить ручные изменения.
	conn := map[string]interface{}{
		"retries":                 0,
		"timeout":                 60,
		"userAgentSuffix":         "",
		"enableCircularRedirects": false,
		"enableCookies":           false,
		"useTrustStore":           false,
	}
	if hc.Connection != nil {
		if hc.Connection.Retries != nil {
			conn["retries"] = *hc.Connection.Retries
		}
		if hc.Connection.Timeout != nil {
			conn["timeout"] = *hc.Connection.Timeout
		}
		conn["userAgentSuffix"] = hc.Connection.UserAgentSuffix
		conn["enableCircularRedirects"] = hc.Connection.EnableCircularRedirects
		conn["enableCookies"] = hc.Connection.EnableCookies
		conn["useTrustStore"] = hc.Connection.UseTrustStore
	}
	result["connection"] = conn

	if hc.Authentication != nil {
		result["authentication"] = hc.Authentication
	}

	return result
}

// buildProxyConfig создаёт конфигурацию для proxy, включая аутентификацию.
func buildProxyConfig(proxy *v1alpha1.ProxyConfig) map[string]interface{} {
	proxyConfig := map[string]interface{}{
		"remoteUrl":      proxy.RemoteUrl,
		"contentMaxAge":  proxy.ContentMaxAge,
		"metadataMaxAge": proxy.MetadataMaxAge,
	}

	return proxyConfig
}

// ListRepositories возвращает список всех репозиториев из Nexus.
func (c *Client) ListRepositories(ctx context.Context) ([]RepositoryListItem, error) {
	c.Logger.Info("Получение списка всех репозиториев")
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetResult(&[]RepositoryListItem{}).
		Get("/service/rest/v1/repositories")
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return *resp.Result().(*[]RepositoryListItem), nil
}

// GetRepositoryByType получает полную конфигурацию репозитория по format/type/name.
func (c *Client) GetRepositoryByType(
	ctx context.Context, format, repoType, name string,
) (map[string]interface{}, error) {
	// Nexus API возвращает format "maven2" в списке репозиториев,
	// но REST endpoint использует "maven" в пути.
	apiFormat := format
	if apiFormat == "maven2" {
		apiFormat = "maven"
	}
	endpoint := fmt.Sprintf("/service/rest/v1/repositories/%s/%s/%s", apiFormat, repoType, name)
	c.Logger.Infof("Получение полной конфигурации репозитория: %s (%s/%s)", name, format, repoType)
	resp, err := c.Resty.R().
		SetContext(ctx).
		Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	if resp.StatusCode() == 404 {
		return nil, ErrRepositoryNotFound
	}
	if resp.StatusCode() != 200 {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("ошибка разбора ответа: %w", err)
	}
	return result, nil
}

// BuildRepositorySpecFromConfig строит RepositorySpec из конфигурации Nexus API.
func BuildRepositorySpecFromConfig(config map[string]interface{}, operatorType string) v1alpha1.RepositorySpec {
	spec := v1alpha1.RepositorySpec{
		Name:   getStringField(config, "name"),
		Type:   operatorType,
		Online: getBoolField(config, "online", true),
	}

	// storage
	if storage, ok := config["storage"].(map[string]interface{}); ok {
		writePolicy := getStringField(storage, "writePolicy")
		if writePolicy == "" {
			writePolicy = "ALLOW"
		}
		spec.Storage = v1alpha1.StorageConfig{
			BlobStoreName:               getStringField(storage, "blobStoreName"),
			StrictContentTypeValidation: getBoolField(storage, "strictContentTypeValidation", true),
			WritePolicy:                 writePolicy,
		}
	}

	// maven
	if maven, ok := config["maven"].(map[string]interface{}); ok {
		spec.Maven = &v1alpha1.MavenConfig{
			VersionPolicy:      getStringField(maven, "versionPolicy"),
			LayoutPolicy:       getStringField(maven, "layoutPolicy"),
			ContentDisposition: getStringField(maven, "contentDisposition"),
		}
	}

	// npm
	if npm, ok := config["npm"].(map[string]interface{}); ok {
		spec.Npm = &v1alpha1.NpmConfig{
			RemoveNonCataloged: getBoolField(npm, "removeNonCataloged", false),
			RemoveQuarantined:  getBoolField(npm, "removeQuarantined", false),
		}
	}

	// docker
	if docker, ok := config["docker"].(map[string]interface{}); ok {
		dc := &v1alpha1.DockerConfig{
			ForceBasicAuth: getBoolField(docker, "forceBasicAuth", false),
			V1Enabled:      getBoolField(docker, "v1Enabled", false),
			Subdomain:      getStringField(docker, "subdomain"),
		}
		if hp, ok := getIntFieldPtr(docker, "httpPort"); ok {
			dc.HttpPort = hp
		}
		if hp, ok := getIntFieldPtr(docker, "httpsPort"); ok {
			dc.HttpsPort = hp
		}
		spec.Docker = dc
	}

	// proxy
	if proxy, ok := config["proxy"].(map[string]interface{}); ok {
		spec.Proxy = &v1alpha1.ProxyConfig{
			RemoteUrl:      strings.TrimSpace(getStringField(proxy, "remoteUrl")),
			ContentMaxAge:  getIntField(proxy, "contentMaxAge"),
			MetadataMaxAge: getIntField(proxy, "metadataMaxAge"),
		}
	}

	// group
	if group, ok := config["group"].(map[string]interface{}); ok {
		gc := &v1alpha1.GroupConfig{}
		if members, ok := group["memberNames"].([]interface{}); ok {
			for _, m := range members {
				if s, ok := m.(string); ok {
					gc.MemberNames = append(gc.MemberNames, s)
				}
			}
		}
		spec.Group = gc
	}

	// routingRuleName
	if rr := getStringField(config, "routingRuleName"); rr != "" {
		spec.RoutingRuleName = rr
	}

	// cleanup
	if cleanup, ok := config["cleanup"].(map[string]interface{}); ok {
		cp := &v1alpha1.CleanupPolicy{}
		if names, ok := cleanup["policyNames"].([]interface{}); ok {
			for _, n := range names {
				if s, ok := n.(string); ok {
					cp.PolicyNames = append(cp.PolicyNames, s)
				}
			}
		}
		spec.Cleanup = cp
	}

	// httpClient
	if hc, ok := config["httpClient"].(map[string]interface{}); ok {
		spec.HttpClient = parseHttpClientConfig(hc)
	}

	// negativeCache
	if nc, ok := config["negativeCache"].(map[string]interface{}); ok {
		spec.NegativeCache = &v1alpha1.NegativeCacheConfig{
			Enabled:    getBoolField(nc, "enabled", true),
			TimeToLive: getIntField(nc, "timeToLive"),
		}
	}

	// apt
	if apt, ok := config["apt"].(map[string]interface{}); ok {
		spec.Apt = &v1alpha1.AptConfig{
			Distribution: getStringField(apt, "distribution"),
			Flat:         getBoolField(apt, "flat", false),
		}
	}

	// aptSigning
	if aptSigning, ok := config["aptSigning"].(map[string]interface{}); ok {
		spec.AptSigning = &v1alpha1.AptSigningConfig{
			Keypair:    getStringField(aptSigning, "keypair"),
			Passphrase: getStringField(aptSigning, "passphrase"),
		}
	}

	// yum
	if yum, ok := config["yum"].(map[string]interface{}); ok {
		spec.Yum = &v1alpha1.YumConfig{
			RepodataDepth: getIntField(yum, "repodataDepth"),
			DeployPolicy:  getStringField(yum, "deployPolicy"),
		}
	}

	// yumSigning
	if yumSigning, ok := config["yumSigning"].(map[string]interface{}); ok {
		spec.YumSigning = &v1alpha1.YumSigningConfig{
			Keypair:    getStringField(yumSigning, "keypair"),
			Passphrase: getStringField(yumSigning, "passphrase"),
		}
	}

	// nugetProxy
	if nugetProxy, ok := config["nugetProxy"].(map[string]interface{}); ok {
		spec.NugetProxy = &v1alpha1.NugetProxyConfig{
			QueryCacheItemMaxAge: getIntField(nugetProxy, "queryCacheItemMaxAge"),
			NugetVersion:         getStringField(nugetProxy, "nugetVersion"),
		}
	}

	// cargo
	if cargo, ok := config["cargo"].(map[string]interface{}); ok {
		spec.Cargo = &v1alpha1.CargoConfig{
			RequireAuthentication: getBoolField(cargo, "requireAuthentication", false),
		}
	}

	// conanProxy
	if conanProxy, ok := config["conanProxy"].(map[string]interface{}); ok {
		spec.ConanProxy = &v1alpha1.ConanProxyConfig{
			ConanVersion: getStringField(conanProxy, "conanVersion"),
		}
	}

	return spec
}

// parseHttpClientConfig парсит блок httpClient из ответа Nexus API.
func parseHttpClientConfig(hc map[string]interface{}) *v1alpha1.HttpClientConfig {
	httpClient := &v1alpha1.HttpClientConfig{
		Blocked:   getBoolField(hc, "blocked", false),
		AutoBlock: getBoolField(hc, "autoBlock", true),
	}
	if conn, ok := hc["connection"].(map[string]interface{}); ok {
		cc := &v1alpha1.ConnectionConfig{
			UserAgentSuffix:         getStringField(conn, "userAgentSuffix"),
			EnableCircularRedirects: getBoolField(conn, "enableCircularRedirects", false),
			EnableCookies:           getBoolField(conn, "enableCookies", false),
			UseTrustStore:           getBoolField(conn, "useTrustStore", false),
		}
		if retries, ok := getIntFieldPtr(conn, "retries"); ok {
			cc.Retries = retries
		}
		if timeout, ok := getIntFieldPtr(conn, "timeout"); ok {
			cc.Timeout = timeout
		}
		httpClient.Connection = cc
	}
	if auth, ok := hc["authentication"].(map[string]interface{}); ok {
		httpClient.Authentication = &v1alpha1.AuthConfig{
			Type:        getStringField(auth, "type"),
			Username:    getStringField(auth, "username"),
			Password:    getStringField(auth, "password"),
			BearerToken: getStringField(auth, "bearerToken"),
		}
	}
	return httpClient
}

func getStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBoolField(m map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return defaultVal
}

func getIntField(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func getIntFieldPtr(m map[string]interface{}, key string) (*int, bool) {
	switch v := m[key].(type) {
	case float64:
		i := int(v)
		if i != 0 {
			return &i, true
		}
	case int:
		if v != 0 {
			return &v, true
		}
	}
	return nil, false
}

// ConfigHasAuthentication проверяет, содержит ли конфигурация репозитория секцию authentication.
// Nexus API не возвращает пароли/токены, поэтому при импорте auth нужно обрабатывать отдельно.
func ConfigHasAuthentication(config map[string]interface{}) bool {
	hc, ok := config["httpClient"].(map[string]interface{})
	if !ok {
		return false
	}
	auth, ok := hc["authentication"].(map[string]interface{})
	if !ok {
		return false
	}
	// Проверяем что authentication не пустой — есть хотя бы type
	if t, ok := auth["type"].(string); ok && t != "" {
		return true
	}
	return false
}

// RepositoryExists проверяет, существует ли репозиторий.
func (c *Client) RepositoryExists(ctx context.Context, name string) (bool, error) {
	c.Logger.Infof("Проверка существования репозитория: %s", name)
	resp, err := c.Resty.R().SetContext(ctx).SetPathParam("name", name).Get("/service/rest/v1/repositories/{name}")
	if err != nil {
		return false, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case 200:
		return true, nil
	case 404:
		return false, nil
	default:
		return false, fmt.Errorf("%w: %d", ErrUnexpectedResponse, resp.StatusCode())
	}
}
