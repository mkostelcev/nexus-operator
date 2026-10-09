# История изменений

## Версия 0.8.14

## Deploy

- stage: вместо preInstallHookImage задан enableWebhook, как у соседнего оператора.
  preInstallHookImage base-чарт не передаёт, а образ хука
  подставляет сам и только при enableWebhook. В 0.8.13 хук остался с заглушкой
  example.com/image_name.

## Версия 0.8.13

## Deploy

- stage: задан preInstallHookImage. С mountSecrets base-чарт добавляет
  pre-install хук, а у него по умолчанию образ-заглушка example.com/image_name:
  выкат 0.8.12 в stage висел на ImagePullBackOff хука до таймаута helm.

## Версия 0.8.12

## Код

- Вебхуки включаются переменной ENABLE_WEBHOOKS, как остальные настройки
  оператора через env. Флаг --enable-webhooks остался, переменная его
  переопределяет.
- RoutingRule: ошибка разбора matcher движком RE2 больше не отказ, а
  предупреждение. Nexus компилирует matchers через java.util.regex.Pattern, а
  RE2 не понимает lookaround, possessive-квантификаторы, \p{javaLowerCase}, \R
  и другое. Из-за этого вебхук отбил бы pypi-block-rule из nexus-pg, который
  Nexus принимает. Пустой matcher теперь отказ, как в Nexus.

## Deploy

- stage: включены валидирующие вебхуки Repository, RoutingRule и
  NexusConfiguration по образцу соседнего оператора. Сертификат выпускает
  cert-manager через самоподписанный Issuer, CA подставляет cainjector.
  failurePolicy Ignore, вебхук ограничен namespace оператора.

## Версия 0.8.11

## Код

- docker-proxy: remoteUrl, contentMaxAge и metadataMaxAge больше не дублируются
  в блоке dockerProxy, они уже есть в общем proxy. Nexus в dockerProxy их не
  хранит и в GET не отдаёт, поэтому configDiff каждый раз находил разницу
  `dockerProxy.contentMaxAge/metadataMaxAge/remoteUrl` и оператор делал PUT теми
  же значениями на каждом reconcile docker-прокси. Тест сравнивает конфиг с
  ответом GET живого Nexus 3.77.

## Версия 0.8.10

## Deploy / Monitoring

- Метрики оператора (порт 8081) вынесены в Service и снимаются через
  ServiceMonitor: до этого аннотация prometheus.io/port указывала на порт
  health-проб, и все правила, кроме NexusOperatorDown, жили без данных
- NexusOperatorSyncErrors ловит любой прирост ошибок синхронизации за 10 минут:
  ошибка Nexus API на невалидную декларацию CR не возвращается из Reconcile
  как error и растёт по одной в 30 секунд, порог rate > 0.1/s её не замечал
- NexusOperatorErrorBudgetBurning считается только при объёме больше 1 sync/s
- Новая метрика nexus_operator_resource_ready{type,resource_namespace,name}: 1 —
  CR синхронизирован, 0 — последняя синхронизация с ошибкой; серия удаляется вместе
  с CR. На ней алерт NexusOperatorResourceNotReady называет конкретный ресурс
- Правила приведены к требованиям pint (dashboard, runbook_url, team, component),
  добавлен runbook deploy/envs/default/monitoring/runbook/nexus-operator.md

## Код

- Клиенты Nexus и Keycloak внедряются в реконсайлеры через встроенную структуру
  ExternalClients; пустое поле означает прежний глобальный синглтон из переменных
  окружения. Тесты подставляют клиент на httptest-сервер и проходят пути
  синхронизации и удаления без сети

## Версия 0.8.9

## Secrets / Deploy

- Исправлено значение пути к секретному секрету пароля Nexus для пользователя оператора

## Версия 0.8.8

## Deploy

- Отключен DR для dmz. Убран ref на CI/CD
- Выдано право на TokenReview служебной учётной записи оператора в dmz:
  Vault обслуживает этот кластер как удалённый и проверяет токен присланным
  же токеном, поэтому без system:auth-delegator секреты не монтируются

## Версия 0.8.7

## Deploy

- Фикс переменных для управления нужным нексусом в dmz

## Версия 0.8.6

## Deploy

- Фикс урла для управления нужным нексусом в dmz

## Версия 0.8.5

## Deploy

- Попытка закастомить неймспейс для раскатки опера

## Версия 0.8.4

### Repository

- **Watch на Secret с кредами прокси-репозиториев** — контроллер Repository теперь следит за target-Secret ESO-интеграции (`ESO_PROXY_AUTH_SECRET`, по умолчанию `nexus-proxy-auth`). При изменении данных секрета (ротация кредов в Vault → пересинк ESO) все Repository с `spec.httpClient.authentication.secretRef` на этот секрет автоматически ставятся в очередь синхронизации — новые креды доезжают до Nexus без рестарта оператора и правок CR
- Фильтр generation/annotations перенесён с глобального `WithEventFilter` на предикаты `For(Repository)` — глобальный фильтр отсекал бы события Secret (у Secret generation при обновлении data не меняется)
- События Secret фильтруются по изменению `data` — обновления только метаданных (labels, annotations) reconcile не триггерят
- Watch регистрируется только при включённой ESO-интеграции (заданы `ESO_SECRET_STORE` и `ESO_VAULT_PATH`) — в окружениях без ESO (например, true-production) informer по Secrets не запускается
- При обновлении секрета в очередь ставятся только репозитории, чьи ключи в `data` реально изменились; в лог пишутся имена изменённых ключей и затронутые репозитории (значения секрета в лог не попадают)

### Логирование

- **В лог обновлений теперь пишется, что именно изменилось** — при апдейте Repository/Privilege/Role/NexusUser/RoutingRule/ContentSelector в записи «успешно обновлен» появилось поле `changed` со списком путей расхождений (например, `httpClient.authentication.password`, `group.memberNames[1]`). Только пути, без значений — пароли и токены утечь не могут
- «Начало обработки …» во всех контроллерах и «Конфигурация актуальна» переведены с Info на debug (`V(1)`) — меньше шума на каждый reconcile
- В записи о создании/обновлении добавлено имя ресурса в Nexus (`name`, для Repository также `type`)
- `resolveAuthFromSecret` на `V(1)` логирует, из какого ключа Secret прочитаны креды и сработал ли fallback по `spec.name`
- Security Realms: в лог обновления добавлен итоговый список активных realm'ов

## Версия 0.8.3

- Исправлен конфиг для true-prod

## Версия 0.8.2

- Выключен функционал ESO в true-prod

## Версия 0.8.1

- Добавлен конфиг оператор для prod окружения

## Версия 0.8.1-RC1

- Добавлен конфиг оператора для cprc окружения 

## Версия 0.8.0

### Leader election

- Добавлен файловый конфиг оператора (`internal/appconfig`), читается из `APP_CONFIG_PATH` (по умолчанию `/config/config.yaml`)
- Параметры leader election (`enabled`, `id`, `leaseDuration`, `renewDeadline`, `retryPeriod`) теперь конфигурируются через блок `config.leaderElection` в `deploy/envs/<env>/values.yaml`
- Дефолт — `enabled: true` во всех окружениях; больше не нужно явно передавать `--leader-elect` в args чарта
- Флаг `--leader-elect` помечен как DEPRECATED и игнорируется (оставлен для совместимости со старыми чартами)
- В `production/values.yaml` добавлен блок `config.leaderElection` с расширенными таймингами (30s/20s/4s) — снижает нагрузку на API server

## Версия 0.7.13

### Настройки деплоя

- В production добавлен параметр `platformDisasterRecovery: true` для катастрофоустойчивого развертывания оператора

## Версия 0.7.12

### Repository

- **HttpClient: принудительный сброс `connection` к desired state** — если `httpClient` указан в CR, оператор всегда отправляет блок `connection` с дефолтами Nexus (`retries: 0`, `timeout: 60`, все bool — `false`). Ручные изменения connection в Nexus UI будут сброшены при следующем reconcile

## Версия 0.7.11

### Repository

- **HttpClient: добавлен блок `connection`** — поддержка настроек HTTP-соединения для proxy-репозиториев: `timeout`, `retries`, `userAgentSuffix`, `enableCircularRedirects`, `enableCookies`, `useTrustStore`. Все поля опциональны — при отсутствии Nexus использует свои дефолты

### CRD

- `ConnectionConfig`: новая структура с полями `retries` (0-10), `timeout` (1-3600), `userAgentSuffix`, `enableCircularRedirects`, `enableCookies`, `useTrustStore`
- `HttpClientConfig`: добавлено поле `Connection *ConnectionConfig`

### Тесты

- Добавлены тесты `BuildRepositorySpecFromConfig` с блоком `connection`
- Добавлен тест `BuildRepositoryConfig` — проверка сериализации `connection` в выходной config

## Версия 0.7.10

### Privilege

- **Privilege: добавлены поля `format` и `actions` для типов `repository-view` и `repository-admin`** — Nexus API требует поле `format` (формат репозитория: maven2, npm, docker, `*`) для привилегий `repository-view` и `repository-admin`. Для `repository-admin` также добавлено поле `actions`. Если `format` не указан — используется дефолт `*`
- **Privilege: исправлены ложные UPDATE при каждом reconcile** — `needsUpdate` сравнивал полные map'ы current и desired без фильтрации. Nexus GET возвращает дополнительные поля (`source`, `readOnly`, `id` и т.д.), из-за чего сравнение всегда находило diff. Теперь current фильтруется до ключей desired (аналогично Repository controller)

### CRD

- `RepositoryViewConfig`: добавлено поле `Format string`
- `RepositoryAdminConfig`: добавлены поля `Format string` и `Actions []string`

### Тесты

- Добавлены тесты `BuildPrivilegeConfig` / `BuildPrivilegeSpecFromAPI` для `repository-admin` (с format и actions)
- Добавлены тесты дефолтного `format = "*"` для `repository-view` и `repository-admin`
- Добавлены тесты `needsUpdate` для privilege controller: лишние поля в current не вызывают diff, изменение description → true, одинаковые конфиги → false

## Версия 0.7.9

### Repository

- Добавлено поле `routingRuleName` в RepositorySpec — позволяет привязать Routing Rule к репозиторию. Поле опциональное, мутабельное, поддерживается в build config (CR → Nexus API) и import (Nexus API → CR)

### Скрипты

- Добавлен скрипт `scripts/patch-routing-rules.sh` — патчит существующие Repository CR, подтягивая `routingRuleName` из Nexus API. Поддерживает `--dry-run` (по умолчанию), `--context`, `--namespace`, `--nexus-url`

## Версия 0.7.8

### Исправления

- Исправлен linter

## Версия 0.7.7

### Исправления

- **Repository: ложные обновления при каждом reconcile** — `needsUpdate` всегда возвращал true из-за несовпадения типов: `BuildRepositoryConfig` создавал конфигурацию с Go-структурами (`*HttpClientConfig`, `StorageConfig` и т.д.), а Nexus API возвращал `map[string]interface{}` с примитивами. Теперь desired-конфигурация нормализуется через JSON round-trip, а current фильтруется до ключей desired (Nexus добавляет `format`, `type`, `replication` и другие служебные поля)
- **Repository: response body в ошибках UpdateRepository** — при ошибке обновления репозитория (400/500 от Nexus) в сообщение теперь включается тело ответа, что упрощает диагностику проблем с репозиториями в FAILED-состоянии
- **NexusConfiguration: HTTP Proxy отправлял null вместо false для httpAuthEnabled** — когда аутентификация прокси не указана в CR, поля `httpAuthEnabled`/`httpsAuthEnabled` отправлялись как `null` (nil pointer) вместо `false`, из-за чего Nexus создавал дефолтную запись `"authentication": {"type": "username", "username": null}` в БД. Теперь: auth не указан или `enabled: false` → `httpAuthEnabled: false`, поля username/password не отправляются; auth `enabled: true` без username → ошибка валидации

### Тесты

- Добавлены тесты для `normalizeConfig`, `filterKeys`, `needsUpdate`: нормализация Go-структур, рекурсивная фильтрация ключей, сравнение идентичных/изменённых конфигураций, игнорирование дополнительных полей Nexus API
- Добавлены тесты для `BuildHttpProxyConfig`: auth disabled → nil authentication, auth enabled без username → ошибка `ErrProxyAuthNoUsername`
- Добавлен тест `TestHttpProxyConfigToExtDirect_NoAuthSendsFalse`: проверяет что `httpAuthEnabled` явно `false` при отсутствии аутентификации

## Версия 0.7.6

### Исправления

- **Repository: fallback поиска ключа credentials в Secret по Nexus-имени** — при импорте репозиториев из Nexus `metadata.name` в K8s может отличаться от `spec.name` (например `repo-cft` vs `repo.cft`). Если ключ не найден в Secret по `secretRef.key` (K8s-имя), оператор теперь пробует найти по `spec.name` (Nexus-имя). Это решает проблему, когда ключи в Vault именуются по оригинальному имени репозитория в Nexus

## Версия 0.7.5

### Исправления

- **Repository: cleanup-политики не применялись к репозиториям в Nexus** — функция `BuildRepositoryConfig` не включала секцию `cleanup` в payload при создании/обновлении репозиториев через Nexus API. Это приводило к тому, что cleanup-политики, указанные в CR (`spec.cleanup.policyNames`), не отправлялись в Nexus, а при каждом update существующие политики сбрасывались из состояния "applied" в "available"

### Тесты

- Добавлены тесты для `BuildRepositoryConfig` с cleanup-политиками: наличие политик, пустой список, отсутствие секции

## Версия 0.7.4

### NexusConfiguration

- HTTP Proxy: REST API `/service/rest/v1/system/http` заменён на ExtDirect API (`coreui_HttpSettings`) — REST endpoint отсутствует в Nexus OSS

### CRD

- Добавлены `additionalPrinterColumns` (Ready, Age) для Repository, ContentSelector, Privilege, RoutingRule, NexusConfiguration
- Repository: добавлен столбец Type

## Версия 0.7.3

### Deploy & Alerts

- Добавлены алерты для оператора
- Кастомизированы пути для проб

## Версия 0.7.2

### NexusConfiguration

- Добавлен RBAC для `nexusconfigurations` в clusterRole
- Добавлен пример CR `examples/cr/nexus-configuration.yaml` (HTTP Proxy + Security Realms)

## Версия 0.7.1

### Deploy

- Добавлена структура `deploy/envs/default` с общими defaults (resources, SYNC_EXISTING_RESOURCES, ESO-переменные)
- Добавлен `deploy/envs/production` с production overrides (NEXUS_URL, ESO_SECRET_STORE, ESO_VAULT_PATH, SYNC_EXISTING_RESOURCES: false)
- Параметризованы отличия dev/production окружений

### NexusUser CRD (breaking change)

- Поле `password` переименовано в `credentials` (`spec.credentials.secretKeyRef`)
- Статус `passwordSynced` переименован в `credentialsSynced`
- Удалён `swagger.json` (не используется в коде)

### Примеры CR

- Добавлены примеры: RoutingRule (BLOCK/ALLOW), Role с keycloakSync, NexusUser
- Исправлены gitleaks-находки: inline пароли в примерах proxy-репозиториев заменены на `<secret>`

## Версия 0.7.0

### Keycloak sync для standalone ролей

- Добавлено поле `keycloakSync` в Role CRD — при `true` оператор создаёт/обновляет client role в Keycloak с описанием "Managed by nexus-operator"
- При удалении Role CRD с `keycloakSync: true` — client role удаляется из Keycloak
- Добавлен condition `KeycloakSynced` в статус Role при `keycloakSync: true`
- Добавлен метод `UpdateClientRole` в Keycloak-клиент для обновления описания ролей
- `EnsureClientRole` теперь принимает параметр `description`

### Исправления

- Nexus API `CreateRole` теперь принимает ответ 200 (помимо 201) как успешный

### NexusTeamBinding

- Удалён CRD `NexusTeamBinding` (управление назначениями через группы Keycloak)

## Версия 0.6.0

### Keycloak-интеграция

- Добавлен контроллер `KeycloakRoleSync` — автоматически создаёт client roles в Keycloak для каждой агрегированной роли из NexusTeamAccess
- Добавлен пакет `pkg/keycloak` — клиент Keycloak Admin REST API (client_credentials auth, CRUD для client roles, управление role-mappings пользователей)
- При удалении NTA — client roles удаляются из Keycloak (через finalizer)
- Конфигурация через env: `KEYCLOAK_URL`, `KEYCLOAK_REALM`, `KEYCLOAK_CLIENT_ID`, `KEYCLOAK_CLIENT_SECRET`, `KEYCLOAK_NEXUS_CLIENT`

### NexusTeamBinding (отключён)

- Добавлен CRD `NexusTeamBinding` — декларативное назначение ролей пользователям через Keycloak
- Контроллер отключён в main.go: назначение ролей на пользователей управляется администраторами через группы Keycloak

### Оптимизация reconcile

- `NexusTeamAccessReconciler`: добавлены `GenerationChangedPredicate` и `AnnotationChangedPredicate` на `For()`, `GenerationChangedPredicate` на `Owns()` — status-only updates дочерних ресурсов больше не триггерят reconcile
- `KeycloakRoleSyncReconciler`: добавлен `GenerationChangedPredicate` — реагирует только на изменения spec NTA
- Dedup для Keycloak sync: хеш списка ролей сохраняется в condition `KeycloakSynced`, при совпадении — sync пропускается
- Периодический resync: NTA — каждые 15 мин, Keycloak — каждые 10 мин (drift detection)

### NexusTeamAccess — улучшения

- Поддержка per-repo `accessLevels` в RepositoryGroup — позволяет задавать разные уровни доступа для разных репозиториев
- Поле `pathPrefix` в RepositoryGroup — кастомный путь для CSEL
- Добавлен импорт NexusTeamAccess из Nexus (`--mode=import`): восстановление teamPath из CSEL expression, группировка привилегий по командам
- Валидация ContentSelector expression (CSEL)

## Версия 0.5.0

- Добавлен CRD `NexusTeamAccess` — один CR на команду, автоматически генерирует все дочерние ресурсы (ContentSelector, Privilege, Role) с правильной иерархией
- Поддержка форматов: docker, maven2, raw, npm, nuget
- Уровни доступа ro/rw/rwd с иерархией (rw включает ro, rwd включает rw) и multi-action привилегиями
- Генерация per-repo ролей и агрегированных ролей на команду
- Автоматическое удаление orphan-ресурсов при изменении spec
- Self-healing: контроллер отслеживает дочерние CR через `Owns()` и восстанавливает при ручном изменении
- Удаление через K8s GC (ownerReferences) — каскадная очистка Nexus существующими контроллерами
- Изменён Go module path.
- Исправлены ошибки golangci-lint (err113, makezero, prealloc, wrapcheck, nilerr, lll) во всех контроллерах
- Общие sentinel-ошибки вынесены в `constants.go`

## Версия 0.4.3

- Добавлены Prometheus-метрики для всех контроллеров
- `nexus_operator_sync_total{type, result}` — счётчик синхронизаций
- `nexus_operator_sync_errors_total{type}` — счётчик ошибок
- `nexus_operator_nexus_api_duration_seconds{method}` — latency Nexus API

## Версия 0.4.2

- Добавлены validating webhooks для Repository и RoutingRule
- Repository: валидация proxy/group/hosted полей по типу репозитория, обязательность maven/docker конфигов
- RoutingRule: валидация mode (BLOCK/ALLOW), matchers (минимум 1, валидный regexp)
- Включается флагом `--enable-webhooks` (по умолчанию выключено)

## Версия 0.4.1

- Добавлены поля `lastSyncTime` и `syncErrors` в статус всех ресурсов (Repository, Role, Privilege, ContentSelector, RoutingRule)
- `lastSyncTime` обновляется при каждой успешной синхронизации, `syncErrors` инкрементируется при ошибках и сбрасывается при успехе

## Версия 0.4.0

- Добавлен новый CRD RoutingRule для управления правилами маршрутизации Nexus
- Поддержка режимов BLOCK и ALLOW с regex-паттернами для матчинга путей
- Добавлен контроллер, Nexus API клиент (CRUD + List) и импорт для RoutingRule

## Версия 0.3.1

- Изменена API-группа CRD: `nexus.platform.alpha.integrations.kostoed.ru` → `nexus.kostoed.ru`

## Версия 0.3.0

- Добавлен режим работы `import` (`--mode=import`) для импорта ресурсов из Nexus в K8s CR
- Поддержка импорта: repositories, roles, privileges, content-selectors
- Флаги: `--namespace`, `--dry-run`, `--skip-builtins`
- Переменные окружения: `OPERATOR_MODE`, `IMPORT_NAMESPACE`
- В режиме import оператор не запускает controller-manager, выполняет импорт и завершается (exit 0)
- Добавлены List-методы для всех ресурсов Nexus API
- Добавлены reverse-build функции для конвертации данных Nexus API в CRD spec
- Добавлена утилита `SanitizeK8sName` для конвертации имён Nexus в RFC 1123

## Версия 0.2.0

- Обновление для Nexus 3.77.2-01
- Удалена поддержка script privilege type (Groovy scripting удалён в Nexus 3.78)
- Добавлены новые actions START и STOP для привилегий
- Добавлена поддержка bearerToken аутентификации для proxy-репозиториев
- Обновлён swagger.json до версии 3.77.2

## Версия 0.1.0

- Третья версия, добавлен минимально необходимый функционал для саппорта по задаче

## Версия 0.0.2

- Выпуск второй версии: добавлена поддержка сущности Repository для типов - maven, npm и docker - proxy и hosted

## Версия 0.0.1

- Выпуск первой версии
