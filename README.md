# Nexus Operator

Kubernetes-оператор для декларативного управления ресурсами Sonatype Nexus Repository Manager через CRD.

## Возможности

- **Репозитории** — создание, обновление, удаление. Форматы: Maven, NPM, Docker, Raw, Helm, PyPI, NuGet, APT, Cargo, Conan и др. Типы: hosted, proxy, group. Аутентификация proxy через `secretRef` (из K8s Secret / Vault + ESO).
- **Роли** — управление ролями с привилегиями и вложенными ролями. Опциональная синхронизация в Keycloak (`keycloakSync: true`).
- **Привилегии** — типы: wildcard, application, repository-view, repository-admin, repository-content-selector.
- **Content Selectors** — JEXL-выражения для контроля доступа к контенту.
- **Routing Rules** — правила маршрутизации запросов (BLOCK/ALLOW с regex-паттернами).
- **NexusConfiguration** — глобальная конфигурация Nexus: HTTP/HTTPS Proxy и Security Realms (активные realm'ы и их приоритет).
- **NexusTeamAccess** — один CR на команду: автоматически генерирует ContentSelector, Privilege и Role для заданных репозиториев и уровней доступа (ro/rw/rwd). Поддержка per-repo accessLevels, pathPrefix, форматов docker/maven2/raw/npm/nuget.
- **Keycloak-интеграция** — контроллер `KeycloakRoleSync` автоматически создаёт client roles в Keycloak для каждой агрегированной роли из NexusTeamAccess. Standalone роли с `keycloakSync: true` также синхронизируются в Keycloak. Назначение ролей на пользователей — через группы Keycloak (администраторами).
- **NexusUser** — управление локальными пользователями Nexus с синхронизацией пароля из Kubernetes Secret.
- **Статус синхронизации** — каждый ресурс содержит `lastSyncTime` и `syncErrors` для наблюдаемости.
- **Validating webhooks** — валидация CR при создании и обновлении (Repository, RoutingRule, NexusConfiguration).
- **Prometheus-метрики** — счётчики синхронизаций, ошибок и latency Nexus API.
- **Режим импорта** — миграция существующих ресурсов Nexus в Kubernetes CR.

## Режимы работы

### Reconcile (по умолчанию)

Стандартный режим оператора: следит за CR в Kubernetes и синхронизирует состояние в Nexus.

```bash
# Запуск (режим по умолчанию)
./nexus-operator
./nexus-operator --mode=reconcile

# С включёнными webhooks
./nexus-operator --enable-webhooks
```

### Import

Обратный режим: читает все ресурсы из Nexus и создаёт соответствующие CR в Kubernetes. Подходит для миграции существующего Nexus под управление оператора. Выполняет импорт и завершается (exit 0) — подходит для запуска как K8s Job.

```bash
# Импорт всех ресурсов
./nexus-operator --mode=import --import-namespace=nexus

# Только посмотреть что будет создано (без записи в K8s)
./nexus-operator --mode=import --import-namespace=nexus --dry-run

# Импортировать включая встроенные ресурсы Nexus
./nexus-operator --mode=import --import-namespace=nexus --skip-builtins=false
```

Порядок импорта (по зависимостям): content-selectors → repositories → privileges → roles → users → routing-rules → nexus-configuration → nexus-team-accesses.

Каждый созданный CR получает:
- Label: `nexus.kostoed.ru/imported: "true"`
- Annotation: `nexus.kostoed.ru/nexus-name: <оригинальное имя>`

**Повторный импорт:** при повторном запуске импорта существующие CR обновляются (а не пропускаются). Это позволяет подтянуть изменения из Nexus (например, обновлённый список `memberNames` в group-репозиториях).

**Авторизация proxy-репозиториев:** Nexus API не возвращает пароли и токены. Если у proxy-репозитория есть authentication, импортёр обнуляет секцию auth и ставит аннотацию `needs-auth-config: "true"`. После настройки аутентификации в Nexus оператор автоматически снимает эту аннотацию (проверяет через format-specific API endpoint, т.к. generic `/repositories/{name}` не возвращает `httpClient`). Найти репозитории, ожидающие настройки аутентификации:

```bash
kubectl get repositories -l nexus.kostoed.ru/imported=true \
  -o json | jq -r '.items[] | select(.metadata.annotations["nexus.kostoed.ru/needs-auth-config"]=="true") | .metadata.name'
```

**Автоматические нормализации при импорте:**

- `proxy.remoteUrl` — обрезаются пробелы по краям (Nexus может хранить URL с trailing space, что вызывает ошибку при создании).
- `repositoryContentSelector.format` — если значение `null` или пустая строка, автоматически подставляется `"*"` (все форматы). Nexus API требует непустое значение.

## Аутентификация proxy-репозиториев

Nexus API не возвращает пароли при чтении конфигурации репозитория. При этом отправка `httpClient` без `authentication` **очищает** существующую аутентификацию в Nexus. Оператор решает эту проблему двумя механизмами:

### Защита от потери аутентификации

Если в Nexus у репозитория настроена аутентификация, а в CR секция `spec.httpClient.authentication` отсутствует — оператор **пропускает обновление** и выставляет статус `AuthPending`:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: AuthPending
      message: "Обновление пропущено: в Nexus настроена аутентификация, отсутствующая в CR. Заполните spec.httpClient.authentication"
```

Найти такие репозитории:

```bash
kubectl get repositories -o json | jq -r '.items[] | select(.status.conditions[]? | .reason == "AuthPending") | .metadata.name'
```

### SecretRef — credentials из Kubernetes Secret

Для управления аутентификацией proxy-репозиториев используйте `secretRef` — ссылку на ключ в Kubernetes Secret, содержащий JSON с username и password:

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: Repository
metadata:
  name: maven-central-proxy
spec:
  name: maven-central-proxy
  type: maven-proxy
  online: true
  storage:
    blobStoreName: default
    strictContentTypeValidation: true
    writePolicy: ALLOW
  proxy:
    remoteUrl: https://repo.maven.apache.org/maven2/
  httpClient:
    autoBlock: true
    blocked: false
    authentication:
      type: username
      secretRef:
        name: nexus-proxy-auth
        key: maven-central-proxy
  negativeCache:
    enabled: true
    timeToLive: 300
```

Secret должен содержать JSON-объект `{"username":"...","password":"..."}` в указанном ключе:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: nexus-proxy-auth
type: Opaque
stringData:
  maven-central-proxy: '{"username":"deploy-user","password":"s3cret"}'
```

`secretRef` имеет приоритет над inline-полями `username` и `password`. При reconcile оператор читает Secret, парсит JSON и подставляет credentials в запрос к Nexus API.

### Интеграция с External Secrets Operator (ESO)

Для хранения credentials в Vault и автоматической доставки в Kubernetes используйте ESO. Рекомендуемая структура в Vault (KV v2):

```
nexus/proxy-auth/maven-central-proxy  → {"username":"deploy-user","password":"s3cret"}
nexus/proxy-auth/npm-registry-proxy   → {"username":"npm-user","password":"token123"}
```

#### Автоматическое создание ExternalSecret

Если заданы переменные `ESO_SECRET_STORE` и `ESO_VAULT_PATH`, оператор автоматически создаёт ExternalSecret при первом обращении к несуществующему Secret. Когда Repository CR ссылается на `secretRef`, а соответствующий K8s Secret ещё не существует — оператор создаёт ExternalSecret (один на namespace) с `dataFrom.find`, который подтянет все credentials из Vault.

**Пререквизиты:**

- В кластере установлен [External Secrets Operator](https://external-secrets.io/)
- Создан `ClusterSecretStore`, указывающий на Vault
- В Vault по указанному пути (`ESO_VAULT_PATH`) лежат секреты в формате `{"username":"...","password":"..."}`

**Включение:**

```bash
export ESO_SECRET_STORE=secret-store-vault   # имя ClusterSecretStore
export ESO_VAULT_PATH=nexus/proxy-auth       # путь в Vault KV v2
# Опционально:
export ESO_PROXY_AUTH_SECRET=nexus-proxy-auth # имя создаваемого Secret (по умолчанию nexus-proxy-auth)
export ESO_REFRESH_INTERVAL=1h               # интервал обновления (по умолчанию 1h)
```

ESO-интеграция включается только когда заданы **оба** обязательных параметра: `ESO_SECRET_STORE` и `ESO_VAULT_PATH`. Без них оператор работает как раньше — если Secret не найден, возвращает ошибку.

**Жизненный цикл:**

```
1. Пользователь создаёт Repository CR с secretRef
2. Оператор пытается прочитать K8s Secret → не найден
3. ESO включён? → Да → создаёт ExternalSecret в namespace CR
4. Reconcile requeue (30 сек)
5. ESO синхронизирует Secret из Vault
6. Следующий reconcile → Secret найден → credentials прочитаны → репозиторий создан в Nexus
```

ExternalSecret создаётся **один на namespace** (по имени из `ESO_PROXY_AUTH_SECRET`). Все proxy-репозитории в одном namespace разделяют один Secret с разными ключами.

**Диагностика:**

```bash
# Проверить статус ExternalSecret
kubectl get externalsecrets -n <namespace>

# Проверить, создан ли Secret
kubectl get secret nexus-proxy-auth -n <namespace>

# Посмотреть ключи в Secret
kubectl get secret nexus-proxy-auth -n <namespace> -o jsonpath='{.data}' | jq 'keys'
```

#### Ручное создание ExternalSecret

Если ESO-интеграция не включена, можно создать ExternalSecret вручную. ExternalSecret с `dataFrom.find` автоматически соберёт все секреты по пути в один K8s Secret:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: nexus-proxy-auth
  namespace: nexus
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: secret-store-vault
    kind: ClusterSecretStore
  target:
    name: nexus-proxy-auth
    creationPolicy: Owner
  dataFrom:
    - find:
        path: nexus/proxy-auth
        name:
          regexp: ".*"
```

Каждый Vault-секрет станет ключом в K8s Secret (имя = последний сегмент пути). CR ссылается на него через `secretRef.key`:

```yaml
spec:
  httpClient:
    authentication:
      type: username
      secretRef:
        name: nexus-proxy-auth
        key: maven-central-proxy
```

## Конфигурация

Пример набора переменных для локального запуска — в `.env.example`.

### Переменные окружения (обязательные)

| Переменная | Описание |
|---|---|
| `NEXUS_URL` | URL сервера Nexus |
| `NEXUS_USER` | Имя пользователя |
| `NEXUS_PASSWORD` | Пароль |

### Переменные окружения (опциональные)

| Переменная | Описание | По умолчанию |
|---|---|---|
| `OPERATOR_MODE` | Режим работы: `reconcile` или `import` | `reconcile` |
| `WATCH_NAMESPACE` | Namespace для watch в reconcile-режиме. По умолчанию — namespace пода | namespace пода |
| `IMPORT_NAMESPACE` | Namespace для создания CR в import-режиме. Пустое значение — все namespace | — |
| `ENABLE_REPOSITORY_DELETION` | Удалять репозитории из Nexus при удалении CR | `false` |
| `ENABLED_CONTROLLERS` | Список контроллеров через запятую (см. [Выборочный запуск контроллеров](#выборочный-запуск-контроллеров)) | все, кроме disabled |
| `ESO_SECRET_STORE` | Имя ClusterSecretStore для ESO-интеграции | — |
| `ESO_VAULT_PATH` | Путь в Vault KV v2 для proxy-auth секретов | — |
| `ESO_PROXY_AUTH_SECRET` | Имя целевого K8s Secret (и ExternalSecret) | `nexus-proxy-auth` |
| `ESO_REFRESH_INTERVAL` | Интервал обновления ExternalSecret | `1h` |
| `KEYCLOAK_URL` | URL Keycloak (например, `https://auth.example.com/auth`) | — |
| `KEYCLOAK_REALM` | Realm для управления ролями | — |
| `KEYCLOAK_CLIENT_ID` | Client ID сервисного аккаунта | — |
| `KEYCLOAK_CLIENT_SECRET` | Client secret | — |
| `KEYCLOAK_NEXUS_CLIENT` | Имя клиента Keycloak для ролей | — |
| `KEYCLOAK_AUTH_REALM` | Realm для токена (если отличается от `KEYCLOAK_REALM`) | = `KEYCLOAK_REALM` |

### Флаги командной строки

| Флаг | Описание | По умолчанию |
|---|---|---|
| `--mode` | Режим работы | `reconcile` |
| `--watch-namespace` | Namespace для watch в reconcile-режиме | namespace пода |
| `--import-namespace` | Namespace для создания CR в import-режиме. Пустое значение — все namespace | — |
| `--dry-run` | Только логировать, не создавать CR | `false` |
| `--skip-builtins` | Пропускать встроенные ресурсы Nexus | `true` |
| `--enable-webhooks` | Включить validating webhooks | `false` |
| `--metrics-bind-address` | Адрес для метрик | `:8081` |
| `--health-probe-bind-address` | Адрес для health-проб | `:8080` |
| `--leader-elect` | Включить leader election | `false` |
| `--controllers` | Список контроллеров через запятую | все, кроме disabled |
| `--dev` | Режим разработки (verbose логи) | `false` |

## Установка

### CRD

```bash
kubectl apply -f config/crd/bases/
```

### Оператор

```bash
# Сборка
go build -o nexus-operator .

# Запуск локально
export NEXUS_URL=https://nexus.example.com
export NEXUS_USER=admin
export NEXUS_PASSWORD=secret
./nexus-operator

# Запуск локально с ESO-интеграцией
# (в целевом кластере должны быть установлены ESO и ClusterSecretStore)
export NEXUS_URL=https://nexus.example.com
export NEXUS_USER=admin
export NEXUS_PASSWORD=secret
export ESO_SECRET_STORE=secret-store-vault
export ESO_VAULT_PATH=nexus/proxy-auth
./nexus-operator
```

При локальном запуске оператор использует kubeconfig (`~/.kube/config`) для подключения к кластеру. ESO-интеграция работает так же, как и при запуске в кластере — оператор создаёт ExternalSecret через K8s API, а ESO (установленный в кластере) синхронизирует его в Secret. Без ESO в кластере ExternalSecret будет создан, но Secret не появится.

### Import как K8s Job

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: nexus-import
spec:
  template:
    spec:
      containers:
        - name: importer
          image: nexus-operator:latest
          args: ["--mode=import", "--import-namespace=nexus", "--dry-run"]
          env:
            - name: NEXUS_URL
              value: "https://nexus.example.com"
            - name: NEXUS_USER
              valueFrom:
                secretKeyRef:
                  name: nexus-credentials
                  key: username
            - name: NEXUS_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: nexus-credentials
                  key: password
      restartPolicy: Never
  backoffLimit: 3
```

## Поддерживаемые типы репозиториев

| Формат | hosted | proxy | group |
|---|---|---|---|
| Maven | maven-hosted | maven-proxy | maven-group |
| NPM | npm-hosted | npm-proxy | npm-group |
| Docker | docker-hosted | docker-proxy | docker-group |
| Raw | raw-hosted | raw-proxy | raw-group |

## Примеры CR

Примеры находятся в [`examples/cr/`](examples/cr/).

### Repository (maven-proxy)

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: Repository
metadata:
  name: example-maven-proxy-repo
spec:
  name: example-maven-proxy-repo
  type: maven-proxy
  online: true
  storage:
    blobStoreName: default
    strictContentTypeValidation: true
    writePolicy: ALLOW
  maven:
    layoutPolicy: STRICT
    versionPolicy: MIXED
  proxy:
    remoteUrl: https://repo.maven.apache.org/maven2/
    contentMaxAge: 1440
    metadataMaxAge: 1440
  httpClient:
    autoBlock: true
    blocked: false
    authentication:                    # опционально
      type: username
      secretRef:                       # credentials из K8s Secret
        name: nexus-proxy-auth
        key: example-maven-proxy-repo
  negativeCache:
    enabled: false
    timeToLive: 300
```

### ContentSelector

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: ContentSelector
metadata:
  name: example-selector
spec:
  name: example-selector
  description: Selects all Java artifacts with version 1.0.x
  expression: format == "maven2" && path =^ "/org/example/.*/1.0.[0-9]+/.*"
```

#### Ограничения regex в CSEL expression

Nexus использует PostgreSQL для хранения данных, а PostgreSQL поддерживает только **POSIX regex** (не Perl-compatible). **Nexus не валидирует regex-паттерны** при создании Content Selector — ошибка возникает позже, при попытке browse/read репозитория, когда PostgreSQL не может выполнить SQL-запрос с невалидным regex. В логах Nexus появляется stacktrace:

```
org.sonatype.nexus.datastore.api.DataAccessException:
### Error querying database. Cause: org.postgresql.util.PSQLException: ERROR: invalid regular expression: quantifier operand invalid
### SQL: SELECT B.*, ... WHERE ... (B.request_path ~ ?) ...
### Cause: org.postgresql.util.PSQLException: ERROR: invalid regular expression: quantifier operand invalid
```

Оператор валидирует CSEL expression и отклоняет неподдерживаемые конструкции **до** отправки в Nexus, предотвращая эту проблему.

**Неподдерживаемые конструкции:**

| Конструкция | Пример | Описание |
|-------------|--------|----------|
| Lookahead/lookbehind | `(?=...)`, `(?!...)`, `(?<=...)`, `(?<!)` | Опережающие/ретроспективные проверки |
| Inline флаги | `(?i)`, `(?m)`, `(?s)`, `(?x)` | Модификаторы внутри regex |
| Non-capturing group | `(?:...)` | Группировка без захвата |
| Atomic group | `(?>...)` | Атомарная группировка |
| Possessive quantifiers | `++`, `*+`, `?+` | Захватывающие квантификаторы |

**Пример ошибки:**

```yaml
# Неправильно — (?i) не поддерживается PostgreSQL
spec:
  expression: path =~ "(?i)example"

# Правильно — используйте character class
spec:
  expression: path =~ "^.*[Ee][Xx][Aa][Mm][Pp][Ll][Ee].*$"
```

При невалидном expression в статусе CR появится ошибка:

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: Error
      message: "невалидный CSEL expression: regex '(?i)example' содержит неподдерживаемую конструкцию: inline флаги (?i, ?m, ?s, ?x) (PostgreSQL использует POSIX regex, не Perl)"
```

### Privilege

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: Privilege
metadata:
  name: example-java-privilege
spec:
  name: example-java-1.0.x-access
  description: Доступ к артефактам Java 1.0.x
  type: repository-content-selector
  repositoryContentSelector:
    actions: [READ, BROWSE]
    contentSelector: example-selector
    format: maven2
    repository: example-maven-hosted-repo
```

### Role

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: Role
metadata:
  name: example-java-role
spec:
  roleId: java-artifacts-access-role
  name: Доступ к Java 1.0.x
  description: Роль предоставляет доступ к артефактам Java 1.0.x
  keycloakSync: true                   # опционально: создать client role в Keycloak
  privileges:
    - example-java-1.0.x-access
    - nx-metrics-all
```

При `keycloakSync: true` оператор создаёт client role в Keycloak с описанием "Managed by nexus-operator" и добавляет condition `KeycloakSynced` в статус:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Success
    - type: KeycloakSynced
      status: "True"
      reason: Success
      message: 'KC role "java-artifacts-access-role" synced'
```

При удалении Role CRD с `keycloakSync: true` — client role удаляется из Keycloak.

### RoutingRule

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: RoutingRule
metadata:
  name: block-internal-artifacts
spec:
  name: block-internal-artifacts
  description: Блокировать доступ к внутренним артефактам
  mode: BLOCK
  matchers:
    - "^/org/internal/.*"
    - "^/com/company/secret/.*"
```

### NexusConfiguration (HTTP Proxy + Security Realms)

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: NexusConfiguration
metadata:
  name: nexus-config
spec:
  httpProxy:
    httpProxy:
      enabled: true
      host: "proxy.corp.local"
      port: "3128"
    httpsProxy:
      enabled: true
      host: "proxy.corp.local"
      port: "3128"
    nonProxyHosts:
      - "localhost"
      - "*.internal"
  securityRealms:
    active:
      - "NexusAuthenticatingRealm"
      - "NexusAuthorizingRealm"
      - "LdapRealm"
      - "DockerToken"
```

Обе секции (`httpProxy` и `securityRealms`) опциональны — можно управлять только одной из них. Если секция не указана, соответствующая настройка Nexus не затрагивается.

При удалении CR: HTTP Proxy сбрасывается в пустую конфигурацию; из активных Security Realms удаляются управляемые realm'ы (при пустом результате остаются `NexusAuthenticatingRealm` и `NexusAuthorizingRealm`).

### NexusTeamAccess

NexusTeamAccess — один CR на команду, который автоматически генерирует все дочерние ресурсы (ContentSelector, Privilege, Role) с правильной иерархией.

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: NexusTeamAccess
metadata:
  name: nta-kostoed-analytics-beta
  namespace: nexus
spec:
  teamPath: kostoed/analytics/beta
  repositories:
    - format: docker
      names:
        - docker-hosted
        - docker-proxy
    - format: maven2
      names:
        - maven-external-staged
      accessLevels: ["ro", "rw"]     # per-repo accessLevels (переопределяют глобальные)
    - format: maven2
      names:
        - maven-public
      accessLevels: ["ro"]           # только чтение для group-репозитория
    - format: raw
      names:
        - raw-hosted
  accessLevels:
    - ro
    - rw
```

**Что генерируется:**

Для каждой комбинации (репозиторий × уровень доступа) создаётся:
1. **ContentSelector** — CSEL expression с путём `teamPath` в формате репозитория
2. **Privilege** — `repository-content-selector` с actions для уровня доступа
3. **Role** (per-repo) — содержит одну привилегию, например `nx-kostoed-analytics-beta-docker-hosted-ro`
4. **Role** (агрегированная) — объединяет все per-repo роли для уровня, например `nexus-kostoed-analytics-beta-ro`

**Уровни доступа:**

| Уровень | Actions |
|---------|---------|
| `ro` | BROWSE, READ |
| `rw` | ADD, BROWSE, EDIT, READ |
| `rwd` | ADD, BROWSE, DELETE, EDIT, READ |

Иерархия: `rw` включает `ro`, `rwd` включает `rw`.

**Per-repo accessLevels:**

Каждый `RepositoryGroup` может иметь свой `accessLevels`, переопределяющий глобальный. Это позволяет, например, давать `rw` на `maven-external-staged`, но только `ro` на `maven-public` (group-репозиторий, куда нельзя писать).

**PathPrefix:**

Опциональное поле `pathPrefix` позволяет задать кастомный путь в CSEL expression вместо стандартного, формируемого из `teamPath`.

**Статус:**

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Success
      message: "NexusTeamAccess успешно синхронизирован"
    - type: KeycloakSynced
      status: "True"
      reason: Synced
      message: "a1b2c3d4e5f6..."    # хеш списка ролей для dedup
  generatedResources:
    - kind: ContentSelector
      name: cs-kostoed-analytics-beta-docker-docker-hosted
      nexusName: cs-kostoed-analytics-beta-docker-docker-hosted
    - kind: Role
      name: nexus-kostoed-analytics-beta-ro
      nexusName: nexus-kostoed-analytics-beta-ro
  lastSyncTime: "2026-03-16T10:00:00Z"
  syncErrors: 0
```

**Удаление:**

Дочерние CR (ContentSelector, Privilege, Role) удаляются через K8s GC (ownerReferences), что запускает каскадное удаление в Nexus существующими контроллерами.

**Self-healing:**

Контроллер отслеживает дочерние CR через `Owns()` и восстанавливает их при ручном изменении или удалении.

**Оптимизация:**

- `GenerationChangedPredicate` на `For()` и `Owns()` — status-only updates дочерних ресурсов не триггерят reconcile
- Периодический resync каждые 15 минут для drift detection
- Первый запуск оператора приводит все ресурсы к декларативному состоянию

### Keycloak-интеграция

Контроллер `KeycloakRoleSync` автоматически создаёт client roles в Keycloak для каждой агрегированной роли из NexusTeamAccess. Это позволяет управлять доступом пользователей к Nexus через группы Keycloak.

**Принцип работы:**

1. При создании/обновлении NTA контроллер извлекает агрегированные роли (с префиксом `nexus-`) из `status.generatedResources`
2. Для каждой роли вызывается `EnsureClientRole` — создаёт роль в Keycloak, если не существует
3. Хеш списка ролей сохраняется в condition `KeycloakSynced` — повторный sync пропускается, если набор ролей не изменился
4. При удалении NTA — client roles удаляются из Keycloak (через finalizer)

**Standalone роли:**

Для ролей, не управляемых NexusTeamAccess (standalone), KC sync включается через `keycloakSync: true` в Role CRD. Оператор создаёт client role с описанием "Managed by nexus-operator", обновляет описание при reconcile, удаляет при удалении CRD.

**Назначение ролей пользователям:**

Назначение ролей на пользователей управляется администраторами через группы Keycloak, не декларативно.

**Конфигурация:**

| Переменная | Описание |
|---|---|
| `KEYCLOAK_URL` | URL Keycloak (например, `https://auth.example.com/auth`) |
| `KEYCLOAK_REALM` | Realm для управления ролями |
| `KEYCLOAK_CLIENT_ID` | Client ID сервисного аккаунта оператора |
| `KEYCLOAK_CLIENT_SECRET` | Client secret |
| `KEYCLOAK_NEXUS_CLIENT` | Имя клиента в Keycloak, для которого создаются roles (например, `nexus`) |
| `KEYCLOAK_AUTH_REALM` | Realm для получения токена (по умолчанию = `KEYCLOAK_REALM`) |

Keycloak env vars опциональны — если не заданы, контроллер `KeycloakRoleSync` не запустится (лог warning при старте).

**Периодический resync:** каждые 10 минут для drift detection.

### NexusUser

> **Экспериментальный контроллер** — по умолчанию отключён. Для включения нужно явно указать через `--controllers` или `ENABLED_CONTROLLERS` (см. [Выборочный запуск контроллеров](#выборочный-запуск-контроллеров)).

NexusUser управляет локальными пользователями Nexus. Пароль читается из Kubernetes Secret.

#### 1. Создать Secret с паролем

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: nexus-user-passwords
  namespace: nexus
type: Opaque
stringData:
  metrics-scraper-password: "s3cureP@ssw0rd"
  ci-deployer-password: "an0therP@ss"
```

Или через kubectl:

```bash
kubectl create secret generic nexus-user-passwords \
  --namespace=nexus \
  --from-literal=metrics-scraper-password='s3cureP@ssw0rd' \
  --from-literal=ci-deployer-password='an0therP@ss'
```

#### 2. Создать NexusUser CR

```yaml
apiVersion: nexus.kostoed.ru/v1alpha1
kind: NexusUser
metadata:
  name: metrics-scraper
  namespace: nexus
spec:
  userId: metrics-scraper          # immutable, нельзя менять после создания
  firstName: Metrics
  lastName: Scraper
  emailAddress: metrics@nexus.local
  status: active                   # active | disabled
  roles:
    - nx-metrics
  credentials:
    secretKeyRef:
      name: nexus-user-passwords
      key: metrics-scraper-password
```

#### Как работает синхронизация пароля

1. Контроллер читает пароль из указанного Secret по `spec.credentials.secretKeyRef`.
2. Если пользователь ещё не существует в Nexus — создаёт его с этим паролем.
3. Если пользователь существует — проверяет текущий пароль через Basic Auth запрос к `/service/rest/v1/status/check`.
4. Если пароль не совпадает — вызывает `PUT /security/users/{userId}/change-password`.
5. Контроллер переопрашивает состояние каждые **5 минут** (`RequeueAfter`), чтобы подхватить изменения пароля в Secret.

#### Статус

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Success
      message: "Пользователь синхронизирован с Nexus"
  lastSyncTime: "2025-01-27T10:30:00Z"
  syncErrors: 0
  credentialsSynced: true
```

| Поле | Описание |
|------|----------|
| `credentialsSynced` | `true` — пароль в Nexus совпадает с Secret. `false` — ошибка при смене пароля |

#### PrintColumns

```bash
$ kubectl get nexususers -n nexus
NAME              USERID            STATUS   READY   AGE
metrics-scraper   metrics-scraper   active   True    5m
ci-deployer       ci-deployer       active   True    3m
```

#### Удаление

При удалении CR пользователь удаляется из Nexus (через финализатор).

#### Импорт существующих пользователей

В режиме `--mode=import` оператор импортирует локальных пользователей (source=default), пропуская admin и anonymous. Так как Nexus API не возвращает пароли, импортированные CR создаются без `spec.credentials` и получают аннотацию `nexus.kostoed.ru/needs-auth-config: "true"`. После импорта нужно вручную:

1. Создать Secret с паролями
2. Добавить `spec.credentials.secretKeyRef` в каждый импортированный CR

```bash
# Найти импортированных пользователей без пароля
kubectl get nexususers -l nexus.kostoed.ru/imported=true \
  -o jsonpath='{range .items[?(@.metadata.annotations.nexus\.kostoed\.ru/needs-auth-config=="true")]}{.metadata.name}{"\n"}{end}'
```

## Выборочный запуск контроллеров

По умолчанию запускаются все контроллеры, кроме помеченных как `disabled` (сейчас это NexusUser).

```bash
# Запустить только Repository и Role
./nexus-operator --controllers=Repository,Role

# Запустить все стандартные + NexusUser
./nexus-operator --controllers=Repository,ContentSelector,Privilege,Role,RoutingRule,NexusConfiguration,NexusUser

# Через переменную окружения
export ENABLED_CONTROLLERS=Repository,Role,NexusUser
./nexus-operator
```

| Контроллер | По умолчанию |
|---|---|
| Repository | включён |
| ContentSelector | включён |
| Privilege | включён |
| Role | включён |
| RoutingRule | включён |
| NexusConfiguration | включён |
| NexusTeamAccess | включён |
| KeycloakRoleSync | включён |
| NexusUser | **отключён** (экспериментальный) |

Если `--controllers` / `ENABLED_CONTROLLERS` задан — запускаются **только** перечисленные контроллеры, включая disabled.

## Статус ресурсов

Каждый управляемый ресурс содержит в `.status`:

| Поле | Тип | Описание |
|---|---|---|
| `conditions` | `[]Condition` | Стандартные K8s conditions. Тип `Ready` — результат последней синхронизации |
| `lastSyncTime` | `*metav1.Time` | Время последней успешной синхронизации с Nexus |
| `syncErrors` | `int32` | Счётчик последовательных ошибок. Сбрасывается в 0 при успешной синхронизации |

Пример статуса:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Success
      message: "Репозиторий успешно синхронизирован"
  lastSyncTime: "2025-01-27T10:30:00Z"
  syncErrors: 0
```

При ошибке синхронизации `syncErrors` инкрементируется, а ресурс переставляется в очередь через 30 секунд.

## Validating Webhooks

Оператор поддерживает validating webhooks для проверки CR при создании и обновлении. Включаются флагом `--enable-webhooks` (по умолчанию выключены).

### Repository

| Тип | Проверка |
|---|---|
| `*-proxy` | `spec.proxy` обязателен, `spec.proxy.remoteUrl` не пуст |
| `*-group` | `spec.group` обязателен, `spec.group.memberNames` не пуст |
| `*-hosted` | `spec.proxy` и `spec.group` должны быть nil |
| `maven-*` | `spec.maven` обязателен |
| `docker-*` | `spec.docker` обязателен |

### RoutingRule

| Проверка |
|---|
| `spec.mode` — только `BLOCK` или `ALLOW` |
| `spec.matchers` — минимум 1 элемент |
| Каждый matcher — валидный regexp |

### NexusConfiguration

| Проверка |
|---|
| `httpProxy.httpProxy`: если `enabled=true` — `host` обязателен |
| `httpProxy.httpsProxy`: если `enabled=true` — `host` обязателен |
| `port` — числовое значение в диапазоне 1-65535 |
| `securityRealms.active` — если указан, не может быть пустым |

## Prometheus-метрики

Оператор экспортирует метрики на `--metrics-bind-address` (по умолчанию `:8081`).

| Метрика | Тип | Labels | Описание |
|---|---|---|---|
| `nexus_operator_sync_total` | Counter | `type`, `result` | Общее число синхронизаций. `type`: repository, role, privilege, contentselector, routingrule, nexusconfiguration, user. `result`: success, error |
| `nexus_operator_sync_errors_total` | Counter | `type` | Общее число ошибок синхронизации по типу ресурса |
| `nexus_operator_nexus_api_duration_seconds` | Histogram | `method` | Latency вызовов Nexus API в секундах. `method`: CreateRepository, UpdateRepository, RepositoryExists и т.д. |

Пример PromQL-запросов:

```promql
# Частота ошибок синхронизации за 5 минут
rate(nexus_operator_sync_errors_total[5m])

# Процент успешных синхронизаций по типу
sum by (type) (rate(nexus_operator_sync_total{result="success"}[5m]))
/ sum by (type) (rate(nexus_operator_sync_total[5m]))

# 99-й перцентиль latency Nexus API
histogram_quantile(0.99, rate(nexus_operator_nexus_api_duration_seconds_bucket[5m]))
```

### ServiceMonitor для Prometheus Operator

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nexus-operator
spec:
  selector:
    matchLabels:
      app: nexus-operator
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
```

## Структура проекта

```
api/v1alpha1/           CRD-типы (Repository, Role, Privilege, ContentSelector, RoutingRule, NexusConfiguration, NexusUser, NexusTeamAccess)
internal/controller/    Reconcile-контроллеры для каждого ресурса + KeycloakRoleSync
internal/importer/      Логика режима import (импорт из Nexus в K8s, включая NexusTeamAccess)
internal/webhook/       Validating webhooks (Repository, RoutingRule, NexusConfiguration)
pkg/nexus/              Клиент Nexus REST API (CRUD + List + Users + HTTP Proxy + Security Realms)
pkg/keycloak/           Клиент Keycloak Admin REST API (client roles, role-mappings, users)
pkg/metrics/            Prometheus-метрики
pkg/utils/              Утилиты (ContainsString, SanitizeK8sName и др.)
config/crd/bases/       CRD-манифесты
config/rbac/            RBAC ClusterRole и role bindings
deploy/                 Helm values
examples/cr/            Примеры Custom Resources
```

## Тестирование

### Запуск тестов

```bash
go test ./... -cover
```

### Структура тестов

Тесты расположены рядом с исходным кодом (стандартная конвенция Go):

```
pkg/utils/utils_test.go                          Утилиты
pkg/nexus/helpers_test.go                         Тестовый хелпер newTestClient (httptest)
pkg/nexus/client_test.go                          NewClient, NewUnexpectedResponseError
pkg/nexus/repository_test.go                      CRUD, BuildRepositoryConfig, BuildRepositorySpecFromConfig, ConfigHasAuthentication
pkg/nexus/role_test.go                            CRUD ролей, BuildRoleConfig
pkg/nexus/user_test.go                            CRUD пользователей, CheckUserAuth, ChangeUserPassword
pkg/nexus/privilege_test.go                       CRUD привилегий, BuildPrivilegeBody
pkg/nexus/contentselector_test.go                 CRUD content-selectors
pkg/nexus/routingrule_test.go                     CRUD routing rules
pkg/nexus/httpproxy_test.go                       HTTP Proxy API
pkg/nexus/securityrealm_test.go                   Security Realms API
internal/controller/nexusteamaccess_controller_test.go  NexusTeamAccess: генерация CS/Privilege/Role, orphan cleanup, per-repo accessLevels
internal/controller/keycloak_rolesync_controller_test.go  extractAggregateRoleNames, NexusTeamBinding CRD fields
internal/importer/filters_test.go                 isBuiltinUser, isBuiltinRole, isBuiltinPrivilege, isSupportedPrivilegeType, nexusFormatTypeToOperatorType
internal/importer/teamaccess_test.go              parseCSContentSelectorName, actionsToAccessLevel, extractTeamPathFromCSEL
internal/webhook/repository_webhook_test.go       Валидация Repository (proxy/group/hosted/maven/docker/yum)
internal/webhook/routingrule_webhook_test.go      Валидация RoutingRule (mode, matchers, regex)
internal/webhook/nexusconfiguration_webhook_test.go  Валидация NexusConfiguration (proxy host/port, security realms)
```

### Подход к тестированию

- **Nexus API клиент** — тесты через `httptest.Server` с мокированием HTTP-ответов. Хелпер `newTestClient` создаёт клиент, подключённый к тестовому серверу.
- **Webhooks** — прямой вызов `ValidateCreate` / `ValidateUpdate` / `ValidateDelete` с построенными CR-объектами.
- **Фильтры импортёра** — table-driven тесты на чистых функциях.
- **Утилиты** — table-driven тесты с граничными случаями.

Все тесты используют [testify](https://github.com/stretchr/testify) для ассертов (`assert.Equal`, `assert.ErrorIs`, `assert.NoError`).

### CI

В CI-пайплайне тесты запускаются командой:

```bash
go test ./... -cover
```

## Диагностика

```bash
# Логи оператора
kubectl logs -l app=nexus-operator -f

# Все управляемые ресурсы
kubectl get repositories,roles,privileges,contentselectors,routingrules,nexusconfigurations,nexusteamaccesses,nexususers

# Условия конкретного ресурса
kubectl describe repository <name>

# Ресурсы с ошибками синхронизации
kubectl get repositories -o jsonpath='{range .items[?(@.status.syncErrors>0)]}{.metadata.name}{"\t"}{.status.syncErrors}{"\n"}{end}'

# Время последней синхронизации
kubectl get repositories -o custom-columns=NAME:.metadata.name,LAST_SYNC:.status.lastSyncTime,ERRORS:.status.syncErrors

# Метрики оператора
curl -s http://localhost:8081/metrics | grep nexus_operator
```
