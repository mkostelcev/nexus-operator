package controller

import (
	"fmt"

	"github.com/mkostelcev/nexus-operator/pkg/keycloak"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
)

// ExternalClients — внедряемые клиенты внешних систем, встраиваются в каждый
// реконсайлер. Пустое поле означает «взять процесс-глобальный синглтон из
// переменных окружения», как было раньше, поэтому main.go менять не нужно.
// Тесты подставляют клиент на httptest-сервер и проходят пути синхронизации
// и удаления без сети.
type ExternalClients struct {
	Nexus    *nexus.Client
	Keycloak *keycloak.Client
}

func (c ExternalClients) nexusClient() (*nexus.Client, error) {
	if c.Nexus != nil {
		return c.Nexus, nil
	}
	client, err := nexus.GetClient()
	if err != nil {
		return nil, fmt.Errorf("клиент Nexus: %w", err)
	}
	return client, nil
}

func (c ExternalClients) keycloakClient() (*keycloak.Client, error) {
	if c.Keycloak != nil {
		return c.Keycloak, nil
	}
	client, err := keycloak.GetClient()
	if err != nil {
		return nil, fmt.Errorf("клиент Keycloak: %w", err)
	}
	return client, nil
}
