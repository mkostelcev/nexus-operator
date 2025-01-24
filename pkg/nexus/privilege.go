// Работа с привилегиями в Sonatype Nexus
package nexus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

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

	if resp.StatusCode() == 404 {
		return nil, ErrPrivilegeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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
	case PrivilegeTypeScript:
		endpoint = "/service/rest/v1/security/privileges/script"
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
	case PrivilegeTypeScript:
		endpoint = fmt.Sprintf("/service/rest/v1/security/privileges/script/%s", name)
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
	resp, _ := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Delete("/service/rest/v1/security/privileges/{name}")

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

	case "repository-admin":
		if spec.RepositoryAdmin == nil {
			return nil, fmt.Errorf("%w", ErrRepoAdminConfigRequired)
		}
		config["repository"] = spec.RepositoryAdmin.Repository

	case "repository-content-selector":
		if spec.RepositoryContentSelector == nil {
			return nil, fmt.Errorf("%w", ErrRepoContentSelConfigRequired)
		}
		config["repository"] = spec.RepositoryContentSelector.Repository
		config["contentSelector"] = spec.RepositoryContentSelector.ContentSelector
		config["format"] = spec.RepositoryContentSelector.Format
		config["actions"] = spec.RepositoryContentSelector.Actions

	case "script":
		if spec.Script == nil {
			return nil, fmt.Errorf("%w", ErrScriptConfigRequired)
		}
		config["scriptName"] = spec.Script.ScriptName

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedPrivilegeType, spec.Type)
	}

	return config, nil
}
