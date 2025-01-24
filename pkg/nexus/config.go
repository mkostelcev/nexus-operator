// Общая конфигурация для клиента Sonatype Nexus
package nexus

import (
	"log"

	"github.com/go-resty/resty/v2"
)

// ConfigureClient настраивает клиента Nexus
func ConfigureClient(baseURL string) *resty.Client {
	log.Println("Настройка клиента Nexus с базовым URL:", baseURL)
	client := resty.New().
		SetBaseURL(baseURL).
		SetDebug(true)
	return client
}
