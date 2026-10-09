// Работа с routing rules в Sonatype Nexus
package nexus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
)

// RoutingRuleXO — структура ответа Nexus API для routing rule.
type RoutingRuleXO struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Mode        string   `json:"mode"`
	Matchers    []string `json:"matchers"`
}

// ListRoutingRules возвращает список всех Routing Rules из Nexus.
func (c *Client) ListRoutingRules(ctx context.Context) ([]RoutingRuleXO, error) {
	c.Logger.Info("Получение списка всех Routing Rules")
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetResult(&[]RoutingRuleXO{}).
		Get(RoutingRuleAPIPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
	return *resp.Result().(*[]RoutingRuleXO), nil
}

// GetRoutingRule получает конфигурацию существующего Routing Rule.
func (c *Client) GetRoutingRule(ctx context.Context, name string) (*RoutingRuleXO, error) {
	logFields := logrus.Fields{
		"component":   "nexus-client",
		"routingRule": name,
	}
	c.Logger.WithFields(logFields).Debug("Получение конфигурации Routing Rule")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Get(RoutingRuleAPIPath + "/{name}")

	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	switch resp.StatusCode() {
	case 200:
		var result RoutingRuleXO
		if err := json.Unmarshal(resp.Body(), &result); err != nil {
			return nil, fmt.Errorf("ошибка разбора ответа: %w", err)
		}
		return &result, nil
	case 404:
		return nil, ErrRoutingRuleNotFound
	default:
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}
}

// RoutingRuleExists проверяет существование Routing Rule.
func (c *Client) RoutingRuleExists(ctx context.Context, name string) (bool, error) {
	_, err := c.GetRoutingRule(ctx, name)
	if errors.Is(err, ErrRoutingRuleNotFound) {
		return false, nil
	}
	return err == nil, err
}

// CreateRoutingRule создаёт новый Routing Rule.
func (c *Client) CreateRoutingRule(ctx context.Context, rule RoutingRuleXO) error {
	logFields := logrus.Fields{
		"component":   "nexus-client",
		"routingRule": rule.Name,
	}
	c.Logger.WithFields(logFields).Info("Создание Routing Rule")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(rule).
		SetHeader("Content-Type", "application/json").
		Post(RoutingRuleAPIPath)

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() >= 200 && resp.StatusCode() < 300 {
		c.Logger.WithFields(logFields).Info("Routing Rule успешно создан")
		return nil
	}

	return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
}

// UpdateRoutingRule обновляет существующий Routing Rule.
func (c *Client) UpdateRoutingRule(ctx context.Context, name string, rule RoutingRuleXO) error {
	logFields := logrus.Fields{
		"component":   "nexus-client",
		"routingRule": name,
	}
	c.Logger.WithFields(logFields).Info("Обновление Routing Rule")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		SetBody(rule).
		SetHeader("Content-Type", "application/json").
		Put(RoutingRuleAPIPath + "/{name}")

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() == 200 || resp.StatusCode() == 204 {
		c.Logger.WithFields(logFields).Info("Routing Rule успешно обновлён")
		return nil
	}

	return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
}

// DeleteRoutingRule удаляет Routing Rule.
func (c *Client) DeleteRoutingRule(ctx context.Context, name string) error {
	logFields := logrus.Fields{
		"component":   "nexus-client",
		"routingRule": name,
	}
	c.Logger.WithFields(logFields).Info("Удаление Routing Rule")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetPathParam("name", name).
		Delete(RoutingRuleAPIPath + "/{name}")

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() == 404 {
		return ErrRoutingRuleNotFound
	}

	if resp.StatusCode() == 204 || resp.StatusCode() == 200 {
		c.Logger.WithFields(logFields).Info("Routing Rule успешно удалён")
		return nil
	}

	return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
}

// BuildRoutingRuleConfig строит RoutingRuleXO из CRD spec.
func BuildRoutingRuleConfig(spec v1alpha1.RoutingRuleSpec) RoutingRuleXO {
	return RoutingRuleXO{
		Name:        spec.Name,
		Description: spec.Description,
		Mode:        spec.Mode,
		Matchers:    spec.Matchers,
	}
}

// BuildRoutingRuleSpecFromAPI строит RoutingRuleSpec из данных Nexus API.
func BuildRoutingRuleSpecFromAPI(rule RoutingRuleXO) v1alpha1.RoutingRuleSpec {
	matchers := rule.Matchers
	if matchers == nil {
		matchers = []string{}
	}
	return v1alpha1.RoutingRuleSpec{
		Name:        rule.Name,
		Description: rule.Description,
		Mode:        rule.Mode,
		Matchers:    matchers,
	}
}
