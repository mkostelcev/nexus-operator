// Работа с репозиториями в Sonatype Nexus
package nexus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// CreateRepository создаёт репозиторий указанного типа.
func (c *Client) CreateRepository(ctx context.Context, repoType string, config map[string]interface{}) error {
	var endpoint string

	switch repoType {
	case TypeMavenHosted:
		endpoint = "/service/rest/v1/repositories/maven/hosted"
	case TypeMavenProxy:
		endpoint = "/service/rest/v1/repositories/maven/proxy"
	case TypeMavenGroup:
		endpoint = "/service/rest/v1/repositories/maven/group"
	case TypeNpmHosted:
		endpoint = "/service/rest/v1/repositories/npm/hosted"
	case TypeNpmProxy:
		endpoint = "/service/rest/v1/repositories/npm/proxy"
	case TypeNpmGroup:
		endpoint = "/service/rest/v1/repositories/npm/group"
	case TypeDockerHosted:
		endpoint = "/service/rest/v1/repositories/docker/hosted"
	case TypeDockerProxy:
		endpoint = "/service/rest/v1/repositories/docker/proxy"
	case TypeDockerGroup:
		endpoint = "/service/rest/v1/repositories/docker/group"
	case TypeRawHosted:
		endpoint = "/service/rest/v1/repositories/raw/hosted"
	case TypeRawProxy:
		endpoint = "/service/rest/v1/repositories/raw/proxy"
	case TypeRawGroup:
		endpoint = "/service/rest/v1/repositories/raw/group"
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedRepoType, repoType)
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
	var endpoint string

	switch repoType {
	case TypeMavenHosted:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/maven/hosted/%s", name)
	case TypeMavenProxy:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/maven/proxy/%s", name)
	case TypeMavenGroup:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/maven/group/%s", name)
	case TypeNpmHosted:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/npm/hosted/%s", name)
	case TypeNpmProxy:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/npm/proxy/%s", name)
	case TypeNpmGroup:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/npm/group/%s", name)
	case TypeDockerHosted:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/docker/hosted/%s", name)
	case TypeDockerProxy:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/docker/proxy/%s", name)
	case TypeDockerGroup:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/docker/group/%s", name)
	case TypeRawHosted:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/raw/hosted/%s", name)
	case TypeRawProxy:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/raw/proxy/%s", name)
	case TypeRawGroup:
		endpoint = fmt.Sprintf("/service/rest/v1/repositories/raw/group/%s", name)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedRepoType, repoType)
	}

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
		return fmt.Errorf("%w: %d", ErrUnexpectedResponse, resp.StatusCode())
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

	if resp.StatusCode() == 404 {
		return nil, ErrRepositoryNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("ошибка запроса: %w", err)
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
		config["httpClient"] = repo.Spec.HttpClient
	}
	if repo.Spec.NegativeCache != nil {
		config["negativeCache"] = repo.Spec.NegativeCache
	}

	// Тип-специфичная конфигурация
	switch repo.Spec.Type {
	case TypeMavenProxy:
		config["maven"] = repo.Spec.Maven
	case TypeMavenHosted:
		config["maven"] = repo.Spec.Maven
	case TypeMavenGroup:
		if repo.Spec.Maven != nil {
			config["maven"] = repo.Spec.Maven
		}
	case TypeNpmHosted:
		config["npm"] = repo.Spec.Npm
	case TypeNpmProxy:
		config["npm"] = repo.Spec.Npm
	case TypeNpmGroup:
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
	case TypeDockerProxy:
		dockerProxyConfig := map[string]interface{}{
			"httpPort":       repo.Spec.Docker.HttpPort,
			"httpsPort":      repo.Spec.Docker.HttpsPort,
			"forceBasicAuth": repo.Spec.Docker.ForceBasicAuth,
			"v1Enabled":      repo.Spec.Docker.V1Enabled,
			"subdomain":      repo.Spec.Docker.Subdomain,
		}

		config["docker"] = dockerProxyConfig
		config["dockerProxy"] = map[string]interface{}{
			"contentMaxAge":  repo.Spec.Proxy.ContentMaxAge,
			"metadataMaxAge": repo.Spec.Proxy.MetadataMaxAge,
			"remoteUrl":      repo.Spec.Proxy.RemoteUrl,
			"indexType":      "REGISTRY",
		}

		if repo.Spec.HttpClient != nil {
			config["httpClient"] = repo.Spec.HttpClient
		}
	case TypeDockerGroup:
		config["group"] = repo.Spec.Group
	case TypeDockerHosted:
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
			config["httpClient"] = repo.Spec.HttpClient
		}
	case TypeRawGroup:
		config["group"] = map[string]interface{}{
			"memberNames": repo.Spec.Group.MemberNames,
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedRepoType, repo.Spec.Type)
	}

	return config, nil
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

// RepositoryExists проверяет, существует ли репозиторий.
func (c *Client) RepositoryExists(ctx context.Context, name string) (bool, error) {
	c.Logger.Infof("Проверка существования репозитория: %s", name)
	resp, err := c.Resty.R().SetPathParam("name", name).Get("/service/rest/v1/repositories/{name}")
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
