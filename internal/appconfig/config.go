// Package appconfig читает файловую конфигурацию оператора (YAML), которая
// монтируется в контейнер из ConfigMap'а и описывается в values деплоя
// под ключом `config:`.
//
// Приоритет источника:
//  1. путь из переменной окружения APP_CONFIG_PATH;
//  2. дефолтный путь /config/config.yaml (куда base-чарт монтирует ConfigMap);
//  3. если файла нет — возвращаются нулевые значения, и применяются дефолты,
//     заданные методами GetXxx() на структурах конфига.
package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"sigs.k8s.io/yaml"
)

// Duration — обёртка над time.Duration с поддержкой человеко-читаемого формата
// в YAML/JSON ("30s", "1h"). Без неё стандартный json.Unmarshal ожидал бы
// число наносекунд, что в values-файлах неудобно.
type Duration struct {
	time.Duration
}

// UnmarshalJSON принимает строку Go-формата (например, "15s") и парсит её
// через time.ParseDuration. Используется sigs.k8s.io/yaml, который пропускает
// YAML через JSON.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("ожидалась строка с длительностью (напр. \"30s\"): %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("распарсить длительность %q: %w", raw, err)
	}
	d.Duration = parsed
	return nil
}

const (
	// configPathEnv — имя env-переменной с путём до конфиг-файла.
	// Совпадает с соглашением соседних операторов.
	configPathEnv = "APP_CONFIG_PATH"

	// defaultClusterConfigPath — путь, куда base-чарт
	// монтирует ConfigMap с содержимым `config:` из values.yaml.
	defaultClusterConfigPath = "/config/config.yaml"
)

// Config — корневая структура конфига оператора. Расширяется по мере появления
// новых конфигурируемых блоков (напр. nexus, keycloak, метрики).
type Config struct {
	// LeaderElection — параметры leader election controller-runtime.
	LeaderElection LeaderElection `json:"leaderElection,omitempty"`
}

// LeaderElection описывает параметры выбора лидера для multi-replica деплоя.
//
// Все поля опциональны — пустой блок означает «использовать дефолты»:
// leader election включён, ID = "nexus-operator-lock", тайминги — из
// controller-runtime (15s/10s/2s). Это безопасно и для replicaCount=1:
// единственная реплика моментально берёт lease сама у себя.
type LeaderElection struct {
	// Enabled — включить leader election. Если nil — считается true.
	// Явный false полезен только для локальной отладки или диагностики
	// инцидентов (split-brain etcd и т.п.) без пересборки образа.
	Enabled *bool `json:"enabled,omitempty"`

	// ID — имя Lease-объекта в namespace оператора.
	// Пусто -> "nexus-operator-lock".
	ID string `json:"id,omitempty"`

	// LeaseDuration — длительность lease, после которой не-лидеры
	// считают, что лидер умер, и пытаются перехватить.
	// Пусто (nil) -> дефолт controller-runtime (15s).
	LeaseDuration *Duration `json:"leaseDuration,omitempty"`

	// RenewDeadline — окно, в течение которого активный лидер должен
	// успеть продлить lease, иначе он сам сложит полномочия.
	// Пусто (nil) -> дефолт controller-runtime (10s).
	RenewDeadline *Duration `json:"renewDeadline,omitempty"`

	// RetryPeriod — интервал, с которым кандидаты пытаются взять/обновить lease.
	// Пусто (nil) -> дефолт controller-runtime (2s).
	RetryPeriod *Duration `json:"retryPeriod,omitempty"`
}

// LeaseDurationPtr возвращает *time.Duration, готовый к передаче
// в ctrl.Options. nil означает «использовать дефолт controller-runtime».
func (l LeaderElection) LeaseDurationPtr() *time.Duration {
	if l.LeaseDuration == nil {
		return nil
	}
	return &l.LeaseDuration.Duration
}

// RenewDeadlinePtr — аналогично LeaseDurationPtr.
func (l LeaderElection) RenewDeadlinePtr() *time.Duration {
	if l.RenewDeadline == nil {
		return nil
	}
	return &l.RenewDeadline.Duration
}

// RetryPeriodPtr — аналогично LeaseDurationPtr.
func (l LeaderElection) RetryPeriodPtr() *time.Duration {
	if l.RetryPeriod == nil {
		return nil
	}
	return &l.RetryPeriod.Duration
}

// IsEnabled возвращает true, если leader election включён.
// Дефолт — true (см. комментарий к полю Enabled).
func (l LeaderElection) IsEnabled() bool {
	if l.Enabled == nil {
		return true
	}
	return *l.Enabled
}

// GetID возвращает имя Lease-объекта или дефолтное значение.
func (l LeaderElection) GetID() string {
	if l.ID == "" {
		return "nexus-operator-lock"
	}
	return l.ID
}

// Load читает и парсит YAML-конфиг. Если файл отсутствует — возвращает
// пустой Config (все методы GetXxx отдадут дефолты). Это нужно для локального
// запуска (go run) и для окружений, где блок `config:` ещё не задан.
func Load() (*Config, error) {
	path := os.Getenv(configPathEnv)
	if path == "" {
		path = defaultClusterConfigPath
	}

	// Путь приходит из ENV/дефолта оператора, а не из пользовательского ввода —
	// G304 здесь неприменим. Файл монтируется в контейнер через ConfigMap.
	data, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		// Отсутствие файла не считаем ошибкой — работаем на дефолтах.
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("прочитать конфиг %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("распарсить конфиг %s: %w", path, err)
	}
	return &cfg, nil
}
