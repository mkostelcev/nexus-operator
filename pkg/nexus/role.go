// Работа с ролями в Sonatype Nexus
package nexus

import (
	"context"
	"errors"
	"fmt"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
)

// GetRole получает информацию о роли
func (c *Client) GetRole(ctx context.Context, roleID string) (*Role, error) {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"role":      roleID,
	}
	c.Logger.WithFields(logFields).Debug("Получение информации о роли")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("id", roleID).
		SetResult(&Role{}).
		Get(RoleAPIPath + "/{id}")

	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case 200:
		return resp.Result().(*Role), nil
	case 404:
		return nil, ErrRoleNotFound
	default:
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
}

// RoleExists проверяет существование роли
func (c *Client) RoleExists(ctx context.Context, roleID string) (bool, error) {
	_, err := c.GetRole(ctx, roleID)
	if errors.Is(err, ErrRoleNotFound) {
		return false, nil
	}
	return err == nil, err
}

// CreateRole создает новую роль
func (c *Client) CreateRole(ctx context.Context, role Role) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"role":      role.ID,
	}
	c.Logger.WithFields(logFields).Info("Создание новой роли")

	exists, err := c.RoleExists(ctx, role.ID)
	if err != nil {
		return fmt.Errorf("ошибка проверки существования роли: %w", err)
	}
	if exists {
		return ErrRoleAlreadyExists
	}

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(role).
		Post(RoleAPIPath)

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() != 201 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	c.Logger.WithFields(logFields).Info("Роль успешно создана")
	return nil
}

// UpdateRole обновляет существующую роль
func (c *Client) UpdateRole(ctx context.Context, roleID string, role Role) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"role":      roleID,
	}
	c.Logger.WithFields(logFields).Info("Обновление роли")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("id", roleID).
		SetBody(role).
		Put(RoleAPIPath + "/{id}")

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() != 204 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	c.Logger.WithFields(logFields).Info("Роль успешно обновлена")
	return nil
}

// DeleteRole удаляет роль
func (c *Client) DeleteRole(ctx context.Context, roleID string) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"role":      roleID,
	}
	c.Logger.WithFields(logFields).Info("Удаление роли")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("id", roleID).
		Delete(RoleAPIPath + "/{id}")

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() == 404 {
		return ErrRoleNotFound
	}

	if resp.StatusCode() != 204 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	c.Logger.WithFields(logFields).Info("Роль успешно удалена")
	return nil
}

// BuildRoleConfig создает конфигурацию для роли из CRD
func BuildRoleConfig(spec v1alpha1.RoleSpec) Role {
	return Role{
		ID:          spec.RoleID,
		Name:        spec.Name,
		Description: spec.Description,
		Privileges:  spec.Privileges,
		Roles:       spec.Roles,
	}
}
