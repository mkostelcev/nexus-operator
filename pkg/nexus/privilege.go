// Работа с привилегиями в Sonatype Nexus
package nexus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// ListPrivileges возвращает список всех привилегий из Nexus.
func (c *Client) ListPrivileges(ctx context.Context) ([]PrivilegeListItem, error) {
	c.Logger.Info("Получение списка всех привилегий")
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetResult(&[]PrivilegeListItem{}).
		Get("/service/rest/v1/security/privileges")
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return *resp.Result().(*[]PrivilegeListItem), nil
}

// BuildPrivilegeSpecFromAPI строит PrivilegeSpec из данных Nexus API (полный GET одной привилегии).
func BuildPrivilegeSpecFromAPI(data map[string]interface{}) v1alpha1.PrivilegeSpec {
	spec := v1alpha1.PrivilegeSpec{
		Name:        getPrivStringField(data, "name"),
		Type:        getPrivStringField(data, "type"),
		Description: getPrivStringField(data, "description"),
	}

	switch spec.Type {
	case PrivilegeTypeWildcard:
		spec.Wildcard = &v1alpha1.WildcardConfig{
			Pattern: getPrivStringField(data, "pattern"),
		}
	case PrivilegeTypeApplication:
		spec.Application = &v1alpha1.ApplicationConfig{
			Domain:  getPrivStringField(data, "domain"),
			Actions: toStringSlice(data["actions"]),
		}
	case PrivilegeTypeRepositoryView:
		format := getPrivStringField(data, "format")
		if format == "" {
			format = "*"
		}
		spec.RepositoryView = &v1alpha1.RepositoryViewConfig{
			Format:     format,
			Repository: getPrivStringField(data, "repository"),
			Actions:    toStringSlice(data["actions"]),
		}
	case PrivilegeTypeRepositoryAdmin:
		format := getPrivStringField(data, "format")
		if format == "" {
			format = "*"
		}
		spec.RepositoryAdmin = &v1alpha1.RepositoryAdminConfig{
			Format:     format,
			Repository: getPrivStringField(data, "repository"),
			Actions:    toStringSlice(data["actions"]),
		}
	case PrivilegeTypeRepositoryContentSelector:
		format := getPrivStringField(data, "format")
		if format == "" {
			format = "*"
		}
		spec.RepositoryContentSelector = &v1alpha1.RepositoryContentSelectorConfig{
			Repository:      getPrivStringField(data, "repository"),
			ContentSelector: getPrivStringField(data, "contentSelector"),
			Format:          format,
			Actions:         toStringSlice(data["actions"]),
		}
	}

	return spec
}

func getPrivStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func (c *Client) PrivilegeExists(ctx context.Context, name string) (bool, error) {
	_, err := c.GetPrivilege(ctx, name)
	if errors.Is(err, ErrPrivilegeNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) GetPrivilege(ctx context.Context, name string) (map[string]interface{}, error) {
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Get("/service/rest/v1/security/privileges/{name}")
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode() == 404 {
		return nil, ErrPrivilegeNotFound
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return result, nil
}

func (c *Client) CreatePrivilege(ctx context.Context, config map[string]interface{}) error {
	privilegeType, ok := config["type"].(string)
	if !ok {
		return ErrInvalidPrivilegeType
	}

	var endpoint string
	switch privilegeType {
	case PrivilegeTypeWildcard:
		endpoint = "/service/rest/v1/security/privileges/wildcard"
	case PrivilegeTypeApplication:
		endpoint = "/service/rest/v1/security/privileges/application"
	case PrivilegeTypeRepositoryView:
		endpoint = "/service/rest/v1/security/privileges/repository-view"
	case PrivilegeTypeRepositoryAdmin:
		endpoint = "/service/rest/v1/security/privileges/repository-admin"
	case PrivilegeTypeRepositoryContentSelector:
		endpoint = "/service/rest/v1/security/privileges/repository-content-selector"
	default:
		return fmt.Errorf("%w: %s", ErrInvalidPrivilegeType, privilegeType)
	}

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(config).
		Post(endpoint)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode() != 201 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return nil
}

func (c *Client) UpdatePrivilege(ctx context.Context, name string, config map[string]interface{}) error {
	privilegeType, ok := config["type"].(string)
	if !ok {
		return ErrInvalidPrivilegeType
	}

	var endpoint string
	switch privilegeType {
	case PrivilegeTypeWildcard:
		endpoint = fmt.Sprintf("/service/rest/v1/security/privileges/wildcard/%s", name)
	case PrivilegeTypeApplication:
		endpoint = fmt.Sprintf("/service/rest/v1/security/privileges/application/%s", name)
	case PrivilegeTypeRepositoryView:
		endpoint = fmt.Sprintf("/service/rest/v1/security/privileges/repository-view/%s", name)
	case PrivilegeTypeRepositoryAdmin:
		endpoint = fmt.Sprintf("/service/rest/v1/security/privileges/repository-admin/%s", name)
	case PrivilegeTypeRepositoryContentSelector:
		endpoint = fmt.Sprintf("/service/rest/v1/security/privileges/repository-content-selector/%s", name)
	default:
		return fmt.Errorf("%w: %s", ErrInvalidPrivilegeType, privilegeType)
	}

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(config).
		Put(endpoint)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode() != 204 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return nil
}

func (c *Client) DeletePrivilege(ctx context.Context, name string) error {
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Delete("/service/rest/v1/security/privileges/{name}")
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() == 404 {
		return ErrPrivilegeNotFound
	}
	if resp.StatusCode() != 204 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return nil
}

func BuildPrivilegeConfig(spec v1alpha1.PrivilegeSpec) (map[string]interface{}, error) {
	config := map[string]interface{}{
		"name":        spec.Name,
		"description": spec.Description,
		"type":        spec.Type,
	}

	switch spec.Type {
	case "wildcard":
		if spec.Wildcard == nil {
			return nil, fmt.Errorf("%w", ErrWildcardConfigRequired)
		}
		config["pattern"] = spec.Wildcard.Pattern

	case "application":
		if spec.Application == nil {
			return nil, fmt.Errorf("%w", ErrApplicationConfigRequired)
		}
		config["domain"] = spec.Application.Domain
		config["actions"] = spec.Application.Actions

	case "repository-view":
		if spec.RepositoryView == nil {
			return nil, fmt.Errorf("%w", ErrRepoViewConfigRequired)
		}
		config["repository"] = spec.RepositoryView.Repository
		config["actions"] = spec.RepositoryView.Actions
		format := spec.RepositoryView.Format
		if format == "" {
			format = "*"
		}
		config["format"] = format

	case "repository-admin":
		if spec.RepositoryAdmin == nil {
			return nil, fmt.Errorf("%w", ErrRepoAdminConfigRequired)
		}
		config["repository"] = spec.RepositoryAdmin.Repository
		config["actions"] = spec.RepositoryAdmin.Actions
		format := spec.RepositoryAdmin.Format
		if format == "" {
			format = "*"
		}
		config["format"] = format

	case "repository-content-selector":
		if spec.RepositoryContentSelector == nil {
			return nil, fmt.Errorf("%w", ErrRepoContentSelConfigRequired)
		}
		config["repository"] = spec.RepositoryContentSelector.Repository
		config["contentSelector"] = spec.RepositoryContentSelector.ContentSelector
		format := spec.RepositoryContentSelector.Format
		if format == "" {
			format = "*"
		}
		config["format"] = format
		config["actions"] = spec.RepositoryContentSelector.Actions

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedPrivilegeType, spec.Type)
	}

	return config, nil
}
