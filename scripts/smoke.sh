#!/usr/bin/env bash
# Smoke test da API: percorre o ciclo de vida completo de um evento.
# Util para demonstrar a aplicacao rodando durante a apresentacao.
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"

step() { printf "\n\033[1;34m==> %s\033[0m\n" "$1"; }

step "Health check"
curl -fsS "$BASE/healthz" | jq .

step "Readiness (valida conexao com o banco)"
curl -fsS "$BASE/readyz" | jq .

step "Criando evento"
CREATED=$(curl -fsS -X POST "$BASE/api/v1/events" \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: demo-apresentacao-1' \
  -d '{
        "name": "Show de Encerramento",
        "description": "Apresentacao final do semestre.",
        "venue": "Centro de Eventos",
        "city": "Ararangua",
        "starts_at": "2027-03-15T22:00:00Z",
        "capacity": 1200,
        "price_cents": 8500
      }')
echo "$CREATED" | jq .
ID=$(echo "$CREATED" | jq -r .id)

step "Buscando por id ($ID)"
curl -fsS "$BASE/api/v1/events/$ID" | jq .

step "Publicando evento"
curl -fsS -X POST "$BASE/api/v1/events/$ID/publish" | jq '{id, status}'

step "Listando eventos publicados em Ararangua"
curl -fsS "$BASE/api/v1/events?status=published&city=Ararangua&limit=5" | jq '{total, items: [.items[] | {name, status}]}'

step "Erro de validacao esperado (422)"
curl -sS -o /dev/null -w "status=%{http_code}\n" -X POST "$BASE/api/v1/events" \
  -H 'Content-Type: application/json' -d '{"name":"","capacity":0}'
curl -sS -X POST "$BASE/api/v1/events" \
  -H 'Content-Type: application/json' -d '{"name":"","capacity":0}' | jq .

step "Erro 404 esperado"
curl -sS -o /dev/null -w "status=%{http_code}\n" \
  "$BASE/api/v1/events/00000000-0000-0000-0000-000000000000"

step "Cancelando e removendo"
curl -fsS -X POST "$BASE/api/v1/events/$ID/cancel" | jq '{id, status}'
curl -sS -o /dev/null -w "delete status=%{http_code}\n" -X DELETE "$BASE/api/v1/events/$ID"

printf "\n\033[1;32mSmoke test concluido.\033[0m\n"
