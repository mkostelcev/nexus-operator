#!/usr/bin/env bash
#
# Патч Repository CR: добавляет routingRuleName из Nexus API в существующие CR.
#
# Использование:
#   ./patch-routing-rules.sh --nexus-url https://nexus.example.com --namespace platform
#   ./patch-routing-rules.sh --nexus-url https://nexus.example.com --namespace platform --apply
#   ./patch-routing-rules.sh --nexus-url https://nexus.example.com --context stage --namespace nexus-stbl --apply
#
# Требования: kubectl, jq, curl
#
set -euo pipefail

# ─── параметры ───────────────────────────────────────────────────
NEXUS_URL=""
NEXUS_USER="${NEXUS_USER:-}"
NEXUS_PWD="${NEXUS_PWD:-}"
NAMESPACE=""
KUBE_CONTEXT=""
APPLY=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --nexus-url)      NEXUS_URL="$2"; shift 2 ;;
        --nexus-user)     NEXUS_USER="$2"; shift 2 ;;
        --nexus-password) NEXUS_PWD="$2"; shift 2 ;;
        --namespace)      NAMESPACE="$2"; shift 2 ;;
        --context)        KUBE_CONTEXT="$2"; shift 2 ;;
        --apply)          APPLY=true; shift ;;
        --dry-run)        APPLY=false; shift ;;
        -h|--help)
            echo "Usage: $0 [--nexus-url <URL>] [--nexus-user <USER>] [--nexus-password <PASS>] [--namespace <NS>] [--context <CTX>] [--apply|--dry-run]"
            echo ""
            echo "  --nexus-url      Nexus base URL (default: https://nexus.kostoed.ru)"
            echo "  --nexus-user     Nexus username (or NEXUS_USER env)"
            echo "  --nexus-password Nexus password (or NEXUS_PWD env)"
            echo "  --namespace      Kubernetes namespace (default: nexus-stbl)"
            echo "  --context        Kubernetes context (default: stage)"
            echo "  --apply          Apply patches (default: dry-run)"
            echo "  --dry-run        Show what would be patched (default)"
            exit 0
            ;;
        *) echo "ERROR: unknown flag: $1" >&2; exit 1 ;;
    esac
done

if [[ -z "$NEXUS_URL" ]]; then
    echo "ERROR: --nexus-url is required" >&2
    exit 1
fi

# ─── Nexus auth ──────────────────────────────────────────────────
CURL_AUTH=()
if [[ -n "$NEXUS_USER" && -n "$NEXUS_PWD" ]]; then
    CURL_AUTH=(-u "${NEXUS_USER}:${NEXUS_PWD}")
elif [[ -n "$NEXUS_USER" ]]; then
    # Пароль не задан — запрашиваем интерактивно
    read -rsp "Nexus password for ${NEXUS_USER}: " NEXUS_PWD
    echo ""
    CURL_AUTH=(-u "${NEXUS_USER}:${NEXUS_PWD}")
fi

# Убираем trailing slash
NEXUS_URL="${NEXUS_URL%/}"

# ─── проверка утилит ──────────────────────────────────────────────
for cmd in kubectl jq curl; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: $cmd не найден" >&2
        exit 1
    fi
done

# ─── kubectl flags ────────────────────────────────────────────────
KUBECTL_FLAGS=()
if [[ -n "$KUBE_CONTEXT" ]]; then
    KUBECTL_FLAGS+=(--context "$KUBE_CONTEXT")
fi
if [[ -n "$NAMESPACE" ]]; then
    KUBECTL_FLAGS+=(-n "$NAMESPACE")
else
    KUBECTL_FLAGS+=(-A)
fi

# ─── sanitize: Nexus name → K8s name ─────────────────────────────
sanitize_k8s_name() {
    local name="$1"
    echo "$name" | tr '.' '-' | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g' | sed 's/^-//;s/-$//' | cut -c1-253
}

# ─── получаем список репозиториев из Nexus ────────────────────────
echo "=== Получение репозиториев из Nexus ==="
NEXUS_REPOS=$(curl -sf "${CURL_AUTH[@]}" "${NEXUS_URL}/service/rest/v1/repositories" 2>/dev/null) || {
    echo "ERROR: не удалось получить список репозиториев из ${NEXUS_URL}" >&2
    exit 1
}

REPO_COUNT=$(echo "$NEXUS_REPOS" | jq 'length')
echo "  Репозиториев в Nexus: ${REPO_COUNT}"
echo ""

# ─── обрабатываем каждый репозиторий ──────────────────────────────
echo "=== Проверка routingRuleName ==="
echo ""

PATCHED=0
SKIPPED=0
FAILED=0
NOT_FOUND=0

for row in $(echo "$NEXUS_REPOS" | jq -r '.[] | @base64'); do
    _jq() { echo "$row" | base64 -d | jq -r "${1}"; }

    REPO_NAME=$(_jq '.name')
    REPO_FORMAT=$(_jq '.format')
    REPO_TYPE=$(_jq '.type')

    # Nexus API: format "maven2" в списке, но "maven" в REST endpoint
    API_FORMAT="$REPO_FORMAT"
    if [[ "$API_FORMAT" == "maven2" ]]; then
        API_FORMAT="maven"
    fi

    # Получаем полную конфигурацию
    FULL_CONFIG=$(curl -sf "${CURL_AUTH[@]}" "${NEXUS_URL}/service/rest/v1/repositories/${API_FORMAT}/${REPO_TYPE}/${REPO_NAME}" 2>/dev/null) || {
        echo "  WARN: не удалось получить конфиг для ${REPO_NAME} (${API_FORMAT}/${REPO_TYPE})" >&2
        FAILED=$((FAILED + 1))
        continue
    }

    ROUTING_RULE=$(echo "$FULL_CONFIG" | jq -r '.routingRuleName // empty')

    # Пропускаем если routingRuleName не задан
    if [[ -z "$ROUTING_RULE" ]]; then
        SKIPPED=$((SKIPPED + 1))
        continue
    fi

    # K8s name
    K8S_NAME=$(sanitize_k8s_name "$REPO_NAME")

    echo "  ${REPO_NAME} → routingRuleName: ${ROUTING_RULE}"
    echo "    K8s name: ${K8S_NAME}"

    if $APPLY; then
        if kubectl "${KUBECTL_FLAGS[@]}" patch repository "$K8S_NAME" --type=merge -p "
spec:
  routingRuleName: ${ROUTING_RULE}
" 2>/dev/null; then
            echo "    STATUS: OK"
            PATCHED=$((PATCHED + 1))
        else
            echo "    STATUS: CR not found or patch failed"
            NOT_FOUND=$((NOT_FOUND + 1))
        fi
    else
        # Dry-run: проверяем что CR существует
        if kubectl "${KUBECTL_FLAGS[@]}" get repository "$K8S_NAME" &>/dev/null; then
            echo "    STATUS: dry-run (будет пропатчен)"
            PATCHED=$((PATCHED + 1))
        else
            echo "    STATUS: CR '${K8S_NAME}' не найден в кластере"
            NOT_FOUND=$((NOT_FOUND + 1))
        fi
    fi
    echo ""
done

# ─── итоги ────────────────────────────────────────────────────────
echo "=== Итого ==="
echo "  Всего репозиториев в Nexus: ${REPO_COUNT}"
echo "  Без routingRuleName (пропущено): ${SKIPPED}"
echo "  С routingRuleName: $((PATCHED + NOT_FOUND + FAILED))"
if $APPLY; then
    echo "  Успешно пропатчено: ${PATCHED}"
else
    echo "  Будет пропатчено: ${PATCHED}"
fi
echo "  CR не найден: ${NOT_FOUND}"
echo "  Ошибок получения конфига: ${FAILED}"
if ! $APPLY; then
    echo ""
    echo "  (dry-run, используйте --apply для применения)"
fi
