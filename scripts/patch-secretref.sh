#!/usr/bin/env bash
#
# Патч Repository CR: добавляет secretRef для репозиториев с аннотацией needs-auth-config=true
#
# Использование:
#   ./patch-secretref.sh              # dry-run: показать что будет пропатчено
#   ./patch-secretref.sh --apply      # применить патчи
#
# Требования: kubectl, jq
#
set -euo pipefail

SECRET_NAME="${SECRET_NAME:-nexus-proxy-auth}"
NAMESPACE="${NAMESPACE:-}"  # пусто = все namespace

APPLY=false
if [[ "${1:-}" == "--apply" ]]; then
    APPLY=true
fi

# ─── проверка утилит ──────────────────────────────────────────────
for cmd in kubectl jq; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: $cmd не найден" >&2
        exit 1
    fi
done

# ─── получаем репозитории с needs-auth-config=true ────────────────
echo "=== Поиск репозиториев с needs-auth-config=true ==="

NS_FLAG=""
if [[ -n "$NAMESPACE" ]]; then
    NS_FLAG="-n $NAMESPACE"
else
    NS_FLAG="-A"
fi

REPOS=$(kubectl get repositories $NS_FLAG -o json | jq -r '
    .items[] |
    select(.metadata.annotations["nexus.kostoed.ru/needs-auth-config"] == "true") |
    select(.spec.proxy.remoteUrl != null) |
    "\(.metadata.namespace)\t\(.metadata.name)\t\(.spec.name)"
')

TOTAL=$(echo "$REPOS" | grep -c . || true)
echo "  Найдено: ${TOTAL}"
echo ""

if [[ $TOTAL -eq 0 ]]; then
    echo "Нет репозиториев для патча."
    exit 0
fi

# ─── патчим ───────────────────────────────────────────────────────
echo "=== Патч репозиториев ==="
echo ""

PATCHED=0
FAILED=0

while IFS=$'\t' read -r ns meta_name spec_name; do
    [[ -z "$meta_name" ]] && continue

    # Ключ в секрете = spec.name (имя репозитория в Nexus)
    secret_key="$spec_name"

    echo "  ${meta_name} (ns: ${ns})"
    echo "    secretRef.name: ${SECRET_NAME}"
    echo "    secretRef.key:  ${secret_key}"

    if $APPLY; then
        # Патчим secretRef
        if kubectl patch repository "$meta_name" -n "$ns" --type=merge -p "
spec:
  httpClient:
    authentication:
      type: username
      secretRef:
        name: ${SECRET_NAME}
        key: ${secret_key}
" 2>/dev/null; then
            # Удаляем аннотацию needs-auth-config
            kubectl annotate repository "$meta_name" -n "$ns" \
                "nexus.kostoed.ru/needs-auth-config-" 2>/dev/null || true
            echo "    STATUS: OK (secretRef добавлен, аннотация удалена)"
            PATCHED=$((PATCHED + 1))
        else
            echo "    STATUS: FAILED"
            FAILED=$((FAILED + 1))
        fi
    else
        echo "    STATUS: dry-run (добавит secretRef, удалит аннотацию)"
    fi
    echo ""
done <<< "$REPOS"

# ─── итоги ────────────────────────────────────────────────────────
echo "=== Итого ==="
echo "  Репозиториев с needs-auth-config: ${TOTAL}"
if $APPLY; then
    echo "  Успешно пропатчено: ${PATCHED}"
    echo "  Ошибок: ${FAILED}"
else
    echo "  (dry-run, используйте --apply для применения)"
fi
