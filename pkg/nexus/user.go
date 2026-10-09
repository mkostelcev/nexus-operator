// Работа с пользователями в Sonatype Nexus
package nexus

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
)

// ListUsers возвращает список всех локальных пользователей из Nexus.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	c.Logger.Info("Получение списка всех пользователей")
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetResult(&[]User{}).
		SetQueryParam("source", "default").
		Get(UserAPIPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return *resp.Result().(*[]User), nil
}

// GetUser получает пользователя по userId, фильтруя по source=default.
func (c *Client) GetUser(ctx context.Context, userId string) (*User, error) {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"userId":    userId,
	}
	c.Logger.WithFields(logFields).Debug("Получение информации о пользователе")

	var users []User
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetResult(&users).
		SetQueryParam("userId", userId).
		SetQueryParam("source", "default").
		Get(UserAPIPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	users = *resp.Result().(*[]User)
	for i := range users {
		if users[i].UserId == userId && users[i].Source == "default" {
			return &users[i], nil
		}
	}

	return nil, ErrUserNotFound
}

// UserExists проверяет существование пользователя.
func (c *Client) UserExists(ctx context.Context, userId string) (bool, error) {
	_, err := c.GetUser(ctx, userId)
	if errors.Is(err, ErrUserNotFound) {
		return false, nil
	}
	return err == nil, err
}

// CreateUser создаёт нового пользователя в Nexus.
func (c *Client) CreateUser(ctx context.Context, user UserCreateRequest) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"userId":    user.UserId,
	}
	c.Logger.WithFields(logFields).Info("Создание нового пользователя")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(user).
		Post(UserAPIPath)
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	c.Logger.WithFields(logFields).Info("Пользователь успешно создан")
	return nil
}

// UpdateUser обновляет атрибуты пользователя (без пароля).
func (c *Client) UpdateUser(ctx context.Context, userId string, user UserUpdateRequest) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"userId":    userId,
	}
	c.Logger.WithFields(logFields).Info("Обновление пользователя")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("userId", userId).
		SetBody(user).
		Put(UserAPIPath + "/{userId}")
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case http.StatusOK, http.StatusNoContent:
		c.Logger.WithFields(logFields).Info("Пользователь успешно обновлён")
		return nil
	case http.StatusNotFound:
		return ErrUserNotFound
	default:
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
}

// DeleteUser удаляет пользователя из Nexus.
func (c *Client) DeleteUser(ctx context.Context, userId string) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"userId":    userId,
	}
	c.Logger.WithFields(logFields).Info("Удаление пользователя")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("userId", userId).
		Delete(UserAPIPath + "/{userId}")
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case http.StatusNoContent:
		c.Logger.WithFields(logFields).Info("Пользователь успешно удалён")
		return nil
	case http.StatusNotFound:
		return ErrUserNotFound
	default:
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
}

// ChangeUserPassword меняет пароль пользователя.
// Nexus API: PUT /security/users/{userId}/change-password с Content-Type: text/plain
func (c *Client) ChangeUserPassword(ctx context.Context, userId, newPassword string) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"userId":    userId,
	}
	c.Logger.WithFields(logFields).Info("Смена пароля пользователя")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("userId", userId).
		SetHeader("Content-Type", "text/plain").
		SetBody(newPassword).
		Put(UserAPIPath + "/{userId}/change-password")
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case http.StatusOK, http.StatusNoContent:
		c.Logger.WithFields(logFields).Info("Пароль успешно изменён")
		return nil
	case http.StatusNotFound:
		return ErrUserNotFound
	default:
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
}

// CheckUserAuth проверяет пароль пользователя через GET /service/rest/v1/status/check с Basic Auth.
func (c *Client) CheckUserAuth(ctx context.Context, userId, password string) bool {
	baseURL := c.Resty.BaseURL

	checkClient, err := NewClient(baseURL, userId, password)
	if err != nil {
		return false
	}

	resp, err := checkClient.Resty.R().
		SetContext(ctx).
		Get("/service/rest/v1/status/check")
	if err != nil {
		return false
	}

	return resp.StatusCode() == http.StatusOK
}

// BuildUserSpecFromAPI строит NexusUserSpec из данных Nexus API (без password).
func BuildUserSpecFromAPI(user User) v1alpha1.NexusUserSpec {
	roles := user.Roles
	if roles == nil {
		roles = []string{}
	}

	return v1alpha1.NexusUserSpec{
		UserId:       user.UserId,
		FirstName:    user.FirstName,
		LastName:     user.LastName,
		EmailAddress: user.EmailAddress,
		Status:       user.Status,
		Roles:        roles,
	}
}
