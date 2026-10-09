// Работа с Security Realms в Sonatype Nexus
package nexus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
)

const (
	SecurityRealmsActiveAPIPath    = "/service/rest/v1/security/realms/active"
	SecurityRealmsAvailableAPIPath = "/service/rest/v1/security/realms/available"
)

// RealmInfo — информация о realm из Nexus API (available realms).
type RealmInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetActiveRealms получает список активных realm'ов из Nexus.
func (c *Client) GetActiveRealms(ctx context.Context) ([]string, error) {
	logFields := logrus.Fields{
		"component": "nexus-client",
	}
	c.Logger.WithFields(logFields).Debug("Получение списка активных Security Realms")

	resp, err := c.Resty.R().
		SetContext(ctx).
		Get(SecurityRealmsActiveAPIPath)

	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	var result []string
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("ошибка разбора ответа: %w", err)
	}
	return result, nil
}

// SetActiveRealms устанавливает список активных realm'ов в Nexus.
func (c *Client) SetActiveRealms(ctx context.Context, realms []string) error {
	logFields := logrus.Fields{
		"component": "nexus-client",
		"realms":    realms,
	}
	c.Logger.WithFields(logFields).Info("Установка активных Security Realms")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(realms).
		SetHeader("Content-Type", "application/json").
		Put(SecurityRealmsActiveAPIPath)

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() == 200 || resp.StatusCode() == 204 {
		c.Logger.WithFields(logFields).Info("Active Security Realms успешно обновлены")
		return nil
	}

	return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
}

// GetAvailableRealms получает список всех доступных realm'ов из Nexus (для валидации).
func (c *Client) GetAvailableRealms(ctx context.Context) ([]RealmInfo, error) {
	logFields := logrus.Fields{
		"component": "nexus-client",
	}
	c.Logger.WithFields(logFields).Debug("Получение списка доступных Security Realms")

	resp, err := c.Resty.R().
		SetContext(ctx).
		SetResult(&[]RealmInfo{}).
		Get(SecurityRealmsAvailableAPIPath)

	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	return *resp.Result().(*[]RealmInfo), nil
}

// BuildSecurityRealmsSpecFromAPI строит SecurityRealmsSpec из данных Nexus API.
func BuildSecurityRealmsSpecFromAPI(activeRealms []string) *v1alpha1.SecurityRealmsSpec {
	if len(activeRealms) == 0 {
		return nil
	}
	realms := make([]string, len(activeRealms))
	copy(realms, activeRealms)
	return &v1alpha1.SecurityRealmsSpec{
		Active: realms,
	}
}

// SecurityRealmsNeedUpdate сравнивает текущий и желаемый список активных realm'ов.
// Порядок важен — определяет приоритет.
func SecurityRealmsNeedUpdate(current, desired []string) bool {
	return !stringSlicesEqual(current, desired)
}
