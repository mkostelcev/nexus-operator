// Работа с HTTP Proxy конфигурацией в Sonatype Nexus через ExtDirect API.
// REST API для HTTP Proxy отсутствует в Nexus OSS — используем внутренний ExtDirect,
// который вызывается из UI (action: coreui_HttpSettings).
package nexus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
)

const (
	ExtDirectPath = "/service/extdirect"

	authTypeUsername = "username"
	authTypeNTLM     = "ntlm"
)

var (
	errExtDirectFailure    = errors.New("ExtDirect ошибка")
	ErrProxyAuthNoUsername = errors.New("аутентификация прокси включена, но username не указан")
)

// --- ExtDirect request / response types ---

// ExtDirectRequest — общая структура запроса ExtDirect RPC.
type ExtDirectRequest struct {
	Action string `json:"action"`
	Method string `json:"method"`
	Data   any    `json:"data"`
	Type   string `json:"type"`
	TID    int    `json:"tid"`
}

// ExtDirectResponse — общая структура ответа ExtDirect RPC.
type ExtDirectResponse struct {
	TID    int             `json:"tid"`
	Action string          `json:"action"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Type   string          `json:"type"`
}

// ExtDirectResult — обёртка результата с success/data.
type ExtDirectResult struct {
	Success bool            `json:"success"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data"`
}

// ExtDirectHttpSettings — структура данных coreui_HttpSettings.
type ExtDirectHttpSettings struct {
	UserAgentSuffix     *string  `json:"userAgentSuffix"`
	Timeout             *int     `json:"timeout"`
	Retries             *int     `json:"retries"`
	HttpEnabled         *bool    `json:"httpEnabled"`
	HttpHost            *string  `json:"httpHost"`
	HttpPort            *int     `json:"httpPort"`
	HttpAuthEnabled     *bool    `json:"httpAuthEnabled"`
	HttpAuthUsername    *string  `json:"httpAuthUsername"`
	HttpAuthPassword    *string  `json:"httpAuthPassword"`
	HttpAuthNtlmHost    *string  `json:"httpAuthNtlmHost"`
	HttpAuthNtlmDomain  *string  `json:"httpAuthNtlmDomain"`
	HttpsEnabled        *bool    `json:"httpsEnabled"`
	HttpsHost           *string  `json:"httpsHost"`
	HttpsPort           *int     `json:"httpsPort"`
	HttpsAuthEnabled    *bool    `json:"httpsAuthEnabled"`
	HttpsAuthUsername   *string  `json:"httpsAuthUsername"`
	HttpsAuthPassword   *string  `json:"httpsAuthPassword"`
	HttpsAuthNtlmHost   *string  `json:"httpsAuthNtlmHost"`
	HttpsAuthNtlmDomain *string  `json:"httpsAuthNtlmDomain"`
	NonProxyHosts       []string `json:"nonProxyHosts"`
}

// --- HttpProxyConfig — прежний интерфейс (используется контроллером) ---

// HttpProxyConfig — структура для контроллера (совместимость).
type HttpProxyConfig struct {
	HttpProxy     *HttpProxySettings `json:"httpProxy,omitempty"`
	HttpsProxy    *HttpProxySettings `json:"httpsProxy,omitempty"`
	NonProxyHosts []string           `json:"nonProxyHosts,omitempty"`
}

// HttpProxySettings — настройки одного прокси-сервера.
type HttpProxySettings struct {
	Enabled        bool                   `json:"enabled"`
	Host           string                 `json:"host"`
	Port           int                    `json:"port"`
	Authentication *HttpProxyAuthSettings `json:"authentication,omitempty"`
}

// HttpProxyAuthSettings — параметры аутентификации прокси.
type HttpProxyAuthSettings struct {
	Type       string `json:"type,omitempty"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	NtlmHost   string `json:"ntlmHost,omitempty"`
	NtlmDomain string `json:"ntlmDomain,omitempty"`
}

// --- Client methods ---

// GetHttpProxy получает текущую конфигурацию HTTP Proxy из Nexus через ExtDirect.
func (c *Client) GetHttpProxy(ctx context.Context) (*HttpProxyConfig, error) {
	logFields := logrus.Fields{"component": "nexus-client"}
	c.Logger.WithFields(logFields).Debug("Получение конфигурации HTTP Proxy (ExtDirect)")

	req := ExtDirectRequest{
		Action: "coreui_HttpSettings",
		Method: "read",
		Data:   nil,
		Type:   "rpc",
		TID:    1,
	}

	var extResp ExtDirectResponse
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(req).
		SetHeader("Content-Type", "application/json").
		SetResult(&extResp).
		Post(ExtDirectPath)

	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса ExtDirect: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	var result ExtDirectResult
	if err := json.Unmarshal(extResp.Result, &result); err != nil {
		return nil, fmt.Errorf("ошибка разбора ExtDirect result: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("%w: %s", errExtDirectFailure, result.Message)
	}

	var settings ExtDirectHttpSettings
	if err := json.Unmarshal(result.Data, &settings); err != nil {
		return nil, fmt.Errorf("ошибка разбора HTTP Settings: %w", err)
	}

	return extDirectToHttpProxyConfig(&settings), nil
}

// UpdateHttpProxy обновляет конфигурацию HTTP Proxy в Nexus через ExtDirect.
func (c *Client) UpdateHttpProxy(ctx context.Context, config HttpProxyConfig) error {
	logFields := logrus.Fields{"component": "nexus-client"}
	c.Logger.WithFields(logFields).Info("Обновление конфигурации HTTP Proxy (ExtDirect)")

	settings := httpProxyConfigToExtDirect(&config)

	req := ExtDirectRequest{
		Action: "coreui_HttpSettings",
		Method: "update",
		Data:   []any{settings},
		Type:   "rpc",
		TID:    2,
	}

	var extResp ExtDirectResponse
	resp, err := c.Resty.R().
		SetContext(ctx).
		SetBody(req).
		SetHeader("Content-Type", "application/json").
		SetResult(&extResp).
		Post(ExtDirectPath)

	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса ExtDirect: %w", err)
	}

	if resp.StatusCode() != 200 {
		return NewUnexpectedResponseError(resp.StatusCode(), resp.String())
	}

	var result ExtDirectResult
	if err := json.Unmarshal(extResp.Result, &result); err != nil {
		return fmt.Errorf("ошибка разбора ExtDirect result: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("%w: %s", errExtDirectFailure, result.Message)
	}

	c.Logger.WithFields(logFields).Info("HTTP Proxy конфигурация успешно обновлена")
	return nil
}

// ResetHttpProxy сбрасывает конфигурацию HTTP Proxy в пустое состояние.
func (c *Client) ResetHttpProxy(ctx context.Context) error {
	return c.UpdateHttpProxy(ctx, HttpProxyConfig{})
}

// --- Converters ---

func extDirectToHttpProxyConfig(s *ExtDirectHttpSettings) *HttpProxyConfig {
	config := &HttpProxyConfig{
		NonProxyHosts: s.NonProxyHosts,
	}

	if s.HttpEnabled != nil && *s.HttpEnabled {
		port := 0
		if s.HttpPort != nil {
			port = *s.HttpPort
		}
		host := ""
		if s.HttpHost != nil {
			host = *s.HttpHost
		}
		settings := &HttpProxySettings{
			Enabled: true,
			Host:    host,
			Port:    port,
		}
		if s.HttpAuthEnabled != nil && *s.HttpAuthEnabled {
			settings.Authentication = &HttpProxyAuthSettings{
				Type:     authTypeUsername,
				Username: ptrStr(s.HttpAuthUsername),
				Password: ptrStr(s.HttpAuthPassword),
			}
			if ptrStr(s.HttpAuthNtlmHost) != "" || ptrStr(s.HttpAuthNtlmDomain) != "" {
				settings.Authentication.Type = authTypeNTLM
				settings.Authentication.NtlmHost = ptrStr(s.HttpAuthNtlmHost)
				settings.Authentication.NtlmDomain = ptrStr(s.HttpAuthNtlmDomain)
			}
		}
		config.HttpProxy = settings
	}

	if s.HttpsEnabled != nil && *s.HttpsEnabled {
		port := 0
		if s.HttpsPort != nil {
			port = *s.HttpsPort
		}
		host := ""
		if s.HttpsHost != nil {
			host = *s.HttpsHost
		}
		settings := &HttpProxySettings{
			Enabled: true,
			Host:    host,
			Port:    port,
		}
		if s.HttpsAuthEnabled != nil && *s.HttpsAuthEnabled {
			settings.Authentication = &HttpProxyAuthSettings{
				Type:     authTypeUsername,
				Username: ptrStr(s.HttpsAuthUsername),
				Password: ptrStr(s.HttpsAuthPassword),
			}
			if ptrStr(s.HttpsAuthNtlmHost) != "" || ptrStr(s.HttpsAuthNtlmDomain) != "" {
				settings.Authentication.Type = authTypeNTLM
				settings.Authentication.NtlmHost = ptrStr(s.HttpsAuthNtlmHost)
				settings.Authentication.NtlmDomain = ptrStr(s.HttpsAuthNtlmDomain)
			}
		}
		config.HttpsProxy = settings
	}

	return config
}

func httpProxyConfigToExtDirect(config *HttpProxyConfig) *ExtDirectHttpSettings {
	s := &ExtDirectHttpSettings{
		NonProxyHosts: config.NonProxyHosts,
	}

	if config.HttpProxy != nil {
		s.HttpEnabled = boolPtr(config.HttpProxy.Enabled)
		s.HttpHost = strPtr(config.HttpProxy.Host)
		s.HttpPort = toIntPtr(config.HttpProxy.Port)
		if config.HttpProxy.Authentication != nil {
			s.HttpAuthEnabled = boolPtr(true)
			s.HttpAuthUsername = strPtr(config.HttpProxy.Authentication.Username)
			s.HttpAuthPassword = strPtr(config.HttpProxy.Authentication.Password)
			s.HttpAuthNtlmHost = strPtr(config.HttpProxy.Authentication.NtlmHost)
			s.HttpAuthNtlmDomain = strPtr(config.HttpProxy.Authentication.NtlmDomain)
		} else {
			s.HttpAuthEnabled = boolPtr(false)
		}
	} else {
		s.HttpEnabled = boolPtr(false)
	}

	if config.HttpsProxy != nil {
		s.HttpsEnabled = boolPtr(config.HttpsProxy.Enabled)
		s.HttpsHost = strPtr(config.HttpsProxy.Host)
		s.HttpsPort = toIntPtr(config.HttpsProxy.Port)
		if config.HttpsProxy.Authentication != nil {
			s.HttpsAuthEnabled = boolPtr(true)
			s.HttpsAuthUsername = strPtr(config.HttpsProxy.Authentication.Username)
			s.HttpsAuthPassword = strPtr(config.HttpsProxy.Authentication.Password)
			s.HttpsAuthNtlmHost = strPtr(config.HttpsProxy.Authentication.NtlmHost)
			s.HttpsAuthNtlmDomain = strPtr(config.HttpsProxy.Authentication.NtlmDomain)
		} else {
			s.HttpsAuthEnabled = boolPtr(false)
		}
	} else {
		s.HttpsEnabled = boolPtr(false)
	}

	return s
}

// --- BuildHttpProxyConfig / BuildHttpProxySpecFromAPI ---

// BuildHttpProxyConfig строит HttpProxyConfig из CRD spec.
// Возвращает ошибку, если аутентификация включена, но username не указан.
func BuildHttpProxyConfig(spec *v1alpha1.HttpProxySpec) (HttpProxyConfig, error) {
	config := HttpProxyConfig{}

	if spec.HttpProxy != nil {
		settings, err := buildProxySettings(spec.HttpProxy, "httpProxy")
		if err != nil {
			return HttpProxyConfig{}, err
		}
		config.HttpProxy = settings
	}

	if spec.HttpsProxy != nil {
		settings, err := buildProxySettings(spec.HttpsProxy, "httpsProxy")
		if err != nil {
			return HttpProxyConfig{}, err
		}
		config.HttpsProxy = settings
	}

	config.NonProxyHosts = spec.NonProxyHosts

	return config, nil
}

// buildProxySettings строит HttpProxySettings из CRD-конфигурации одного прокси-сервера.
func buildProxySettings(server *v1alpha1.HttpProxyServerConfig, label string) (*HttpProxySettings, error) {
	port, _ := strconv.Atoi(server.Port)
	settings := &HttpProxySettings{
		Enabled: server.Enabled,
		Host:    server.Host,
		Port:    port,
	}
	if server.AuthInfo != nil && server.AuthInfo.Enabled {
		if server.AuthInfo.Username == "" {
			return nil, fmt.Errorf("%w: %s", ErrProxyAuthNoUsername, label)
		}
		authType := authTypeUsername
		if server.AuthInfo.NtlmHost != "" || server.AuthInfo.NtlmDomain != "" {
			authType = authTypeNTLM
		}
		settings.Authentication = &HttpProxyAuthSettings{
			Type:       authType,
			Username:   server.AuthInfo.Username,
			Password:   server.AuthInfo.Password,
			NtlmHost:   server.AuthInfo.NtlmHost,
			NtlmDomain: server.AuthInfo.NtlmDomain,
		}
	}
	return settings, nil
}

// BuildHttpProxySpecFromAPI строит HttpProxySpec из данных Nexus API.
func BuildHttpProxySpecFromAPI(config *HttpProxyConfig) *v1alpha1.HttpProxySpec {
	if config == nil {
		return nil
	}

	spec := &v1alpha1.HttpProxySpec{}

	if config.HttpProxy != nil {
		spec.HttpProxy = &v1alpha1.HttpProxyServerConfig{
			Enabled: config.HttpProxy.Enabled,
			Host:    config.HttpProxy.Host,
			Port:    strconv.Itoa(config.HttpProxy.Port),
		}
		if config.HttpProxy.Authentication != nil {
			spec.HttpProxy.AuthInfo = &v1alpha1.ProxyAuthConfig{
				Enabled:    true,
				Username:   config.HttpProxy.Authentication.Username,
				Password:   config.HttpProxy.Authentication.Password,
				NtlmHost:   config.HttpProxy.Authentication.NtlmHost,
				NtlmDomain: config.HttpProxy.Authentication.NtlmDomain,
			}
		}
	}

	if config.HttpsProxy != nil {
		spec.HttpsProxy = &v1alpha1.HttpProxyServerConfig{
			Enabled: config.HttpsProxy.Enabled,
			Host:    config.HttpsProxy.Host,
			Port:    strconv.Itoa(config.HttpsProxy.Port),
		}
		if config.HttpsProxy.Authentication != nil {
			spec.HttpsProxy.AuthInfo = &v1alpha1.ProxyAuthConfig{
				Enabled:    true,
				Username:   config.HttpsProxy.Authentication.Username,
				Password:   config.HttpsProxy.Authentication.Password,
				NtlmHost:   config.HttpsProxy.Authentication.NtlmHost,
				NtlmDomain: config.HttpsProxy.Authentication.NtlmDomain,
			}
		}
	}

	spec.NonProxyHosts = config.NonProxyHosts

	return spec
}

// HttpProxyNeedsUpdate сравнивает текущую и желаемую конфигурации HTTP Proxy.
func HttpProxyNeedsUpdate(current, desired *HttpProxyConfig) bool {
	if !httpProxySettingsEqual(current.HttpProxy, desired.HttpProxy) {
		return true
	}
	if !httpProxySettingsEqual(current.HttpsProxy, desired.HttpsProxy) {
		return true
	}
	if !stringSlicesEqual(current.NonProxyHosts, desired.NonProxyHosts) {
		return true
	}
	return false
}

func httpProxySettingsEqual(current, desired *HttpProxySettings) bool {
	if current == nil && desired == nil {
		return true
	}
	if current == nil || desired == nil {
		return false
	}
	if current.Enabled != desired.Enabled ||
		current.Host != desired.Host ||
		current.Port != desired.Port {
		return false
	}
	if desired.Authentication != nil && desired.Authentication.Password != "" {
		return false
	}
	if !httpProxyAuthEqual(current.Authentication, desired.Authentication) {
		return false
	}
	return true
}

func httpProxyAuthEqual(current, desired *HttpProxyAuthSettings) bool {
	if current == nil && desired == nil {
		return true
	}
	if current == nil || desired == nil {
		return false
	}
	return current.Username == desired.Username &&
		current.NtlmHost == desired.NtlmHost &&
		current.NtlmDomain == desired.NtlmDomain
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Pointer helpers ---

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func toIntPtr(i int) *int     { return &i }
