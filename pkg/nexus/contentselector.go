// Работа с content-selectors в Sonatype Nexus
package nexus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
)

// GetContentSelector получает конфигурацию существующего Content Selector
func (c *Client) GetContentSelector(ctx context.Context, name string) (*ContentSelectorResponse, error) {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"selector":  name,
	}
	c.Logger.WithFields(logFields).Debug("Получение конфигурации Content Selector")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Get("/service/rest/v1/security/content-selectors/{name}")

	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case 200:
		var result ContentSelectorResponse
		if err := json.Unmarshal(resp.Body(), &result); err != nil {
			return nil, fmt.Errorf("ошибка разбора ответа: %w", err)
		}
		return &result, nil
	case 404:
		return nil, ErrContentSelectorNotFound
	default:
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
}

// CreateContentSelector создает новый Content Selector
func (c *Client) CreateContentSelector(ctx context.Context, name, description, expression string) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"selector":  name,
	}
	c.Logger.WithFields(logFields).Info("Создание Content Selector")

	// Проверка существования перед созданием
	exists, err := c.ContentSelectorExists(ctx, name)
	if err != nil {
		return fmt.Errorf("ошибка проверки существования: %w", err)
	}
	if exists {
		return ErrContentSelectorAlreadyExists
	}

	body := map[string]string{
		"name":        name,
		"description": description,
		"expression":  expression,
	}

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(body).
		SetHeader("Content-Type", "application/json").
		Post("/service/rest/v1/security/content-selectors")

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	// Обрабатываем все успешные статусы 2xx
	if resp.StatusCode() >= 200 && resp.StatusCode() < 300 {
		c.Logger.WithFields(logFields).Info("Content Selector успешно создан")
		return nil
	}

	return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
}

// UpdateContentSelector обновляет существующий Content Selector
func (c *Client) UpdateContentSelector(ctx context.Context, name, description, expression string) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"selector":  name,
	}
	c.Logger.WithFields(logFields).Info("Обновление Content Selector")

	body := map[string]string{
		"description": description,
		"expression":  expression,
	}

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		SetBody(body).
		SetHeader("Content-Type", "application/json").
		Put("/service/rest/v1/security/content-selectors/{name}")

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	// Обрабатываем успешные статусы 200 и 204
	if resp.StatusCode() == 200 || resp.StatusCode() == 204 {
		c.Logger.WithFields(logFields).Info("Content Selector успешно обновлен")
		return nil
	}

	return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
}

// DeleteContentSelector удаляет Content Selector
func (c *Client) DeleteContentSelector(ctx context.Context, name string) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"selector":  name,
	}
	c.Logger.WithFields(logFields).Info("Удаление Content Selector")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Delete("/service/rest/v1/security/content-selectors/{name}")

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() == 404 {
		return ErrContentSelectorNotFound
	}

	if resp.StatusCode() == 204 || resp.StatusCode() == 200 {
		c.Logger.WithFields(logFields).Info("Content Selector успешно удален")
		return nil
	}

	return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
}

// ContentSelectorExists проверяет существование Content Selector
func (c *Client) ContentSelectorExists(ctx context.Context, name string) (bool, error) {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"selector":  name,
	}
	c.Logger.WithFields(logFields).Debug("Проверка существования Content Selector")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Head("/service/rest/v1/security/content-selectors/{name}")

	if err != nil {
		return false, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case 200:
		return true, nil
	case 404:
		return false, nil
	default:
		return false, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
}
