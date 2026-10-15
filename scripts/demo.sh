#!/usr/bin/env bash
# Menu interativo para demonstrar a API durante a apresentacao.
# Cada opcao mostra a requisicao, o status HTTP e a resposta.
# Uso: ./scripts/demo.sh   (ou: make demo)
set -u

BASE="${BASE:-http://localhost:8080}"
API="$BASE/api/v1"
LAST_ID=""

BOLD=$'\033[1m'; BLUE=$'\033[1;34m'; GREEN=$'\033[1;32m'
YELLOW=$'\033[1;33m'; RED=$'\033[1;31m'; DIM=$'\033[2m'; RESET=$'\033[0m'

# Formata JSON se o jq existir; senao imprime cru.
pretty() {
  if command -v jq >/dev/null 2>&1; then jq . 2>/dev/null || cat; else cat; fi
}

# call METODO URL [JSON]: mostra requisicao, status colorido e corpo.
call() {
  local method="$1" url="$2" body="${3:-}" out status
  printf "\n%s→ %s %s%s\n" "$BLUE" "$method" "$url" "$RESET"
  [ -n "$body" ] && printf "%s%s%s\n" "$DIM" "$body" "$RESET"

  local args=(-sS -X "$method" -w $'\n%{http_code}' "$url")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

  if ! out=$(curl "${args[@]}" 2>&1); then
    printf "%sNao foi possivel conectar. O servico esta no ar? (make up)%s\n" "$RED" "$RESET"
    return 1
  fi
  status="${out##*$'\n'}"
  RESPONSE="${out%$'\n'*}"

  local color="$GREEN"
  case "$status" in 4*) color="$YELLOW" ;; 5*) color="$RED" ;; esac
  printf "%sStatus: %s%s\n" "$color" "$status" "$RESET"
  [ -n "$RESPONSE" ] && printf "%s\n" "$RESPONSE" | pretty
  return 0
}

# Extrai o id da ultima resposta (sem depender do jq).
remember_id() {
  local id
  id=$(printf "%s" "$RESPONSE" | grep -o '"id":"[^"]*"' | head -n1 | cut -d'"' -f4)
  [ -n "$id" ] && LAST_ID="$id"
}

# ask "pergunta" "padrao" -> ecoa a resposta (ou o padrao se vazio)
ask() {
  local reply
  read -r -p "$1 [$2]: " reply
  printf "%s" "${reply:-$2}"
}

# Pede um id, sugerindo o ultimo criado.
ask_id() {
  local def="${LAST_ID:-00000000-0000-0000-0000-000000000000}"
  ask "ID do evento" "$def"
}

future_date() {
  date -d "+90 days" +"%d/%m/%Y" 2>/dev/null || echo "15/03/2027"
}

health() {
  call GET "$BASE/healthz"
  call GET "$BASE/readyz"
}

list_events() {
  local city status opt query=""
  city=$(ask "Cidade" "todas")
  [ "$city" = "todas" ] && city=""

  printf "Status: 1) todos  2) rascunho  3) publicado  4) cancelado\n"
  opt=$(ask "Escolha" "1")
  case "$opt" in
    2) status="draft" ;;
    3) status="published" ;;
    4) status="cancelled" ;;
    *) status="" ;;
  esac

  [ -n "$city" ] && query="city=$city"
  [ -n "$status" ] && query="${query:+$query&}status=$status"
  call GET "$API/events${query:+?$query}"
}

create_event() {
  local name venue city starts cap price
  printf "%sNovo evento (Enter aceita o valor entre colchetes)%s\n" "$BOLD" "$RESET"
  name=$(ask "Nome" "Show de Encerramento")
  venue=$(ask "Local" "Centro de Eventos")
  city=$(ask "Cidade" "Ararangua")
  starts=$(ask "Data (dd/mm/aaaa)" "$(future_date)")
  # A API espera RFC3339; convertemos dd/mm/aaaa para esse formato.
  if [[ "$starts" =~ ^([0-9]{2})/([0-9]{2})/([0-9]{4})$ ]]; then
    starts="${BASH_REMATCH[3]}-${BASH_REMATCH[2]}-${BASH_REMATCH[1]}T20:00:00Z"
  else
    printf "%sData fora do formato dd/mm/aaaa; enviando como digitada.%s\n" "$YELLOW" "$RESET"
  fi
  cap=$(ask "Capacidade" "1200")
  price=$(ask "Preco em reais (ex: 85 ou 85,50)" "85")
  # A API guarda o preco em centavos (inteiro), entao convertemos aqui.
  price=$(printf "%s" "${price//,/.}" | awk '{ printf "%d", $1 * 100 + 0.5 }')

  call POST "$API/events" "{\"name\":\"$name\",\"description\":\"Criado pela demo\",\"venue\":\"$venue\",\"city\":\"$city\",\"starts_at\":\"$starts\",\"capacity\":$cap,\"price_cents\":$price}" || return
  remember_id
  [ -n "$LAST_ID" ] && printf "\n%sID guardado para as proximas opcoes: %s%s\n" "$GREEN" "$LAST_ID" "$RESET"
}

get_event()     { call GET    "$API/events/$(ask_id)"; }
publish_event() { call POST   "$API/events/$(ask_id)/publish"; }
cancel_event()  { call POST   "$API/events/$(ask_id)/cancel"; }
delete_event()  { call DELETE "$API/events/$(ask_id)"; }

lookup_cep() {
  call GET "$API/addresses/$(ask "CEP" "01001000")"
}

show_errors() {
  printf "\n%s== Erros de negocio ==%s\n" "$BOLD" "$RESET"
  printf "%sCEP invalido (esperado 400)%s" "$DIM" "$RESET";      call GET "$API/addresses/123"
  printf "%sCEP inexistente (esperado 404)%s" "$DIM" "$RESET";   call GET "$API/addresses/88900000"
  printf "%sEvento inexistente (esperado 404)%s" "$DIM" "$RESET"; call GET "$API/events/00000000-0000-0000-0000-000000000000"
  printf "%sID mal formado (esperado 400)%s" "$DIM" "$RESET";    call GET "$API/events/abc"
  printf "%sPayload vazio (esperado 422)%s" "$DIM" "$RESET";     call POST "$API/events" '{"name":"","capacity":0}'
}

menu() {
  printf "\n%s================ events-service ================%s\n" "$BOLD" "$RESET"
  printf "  API: %s   |   ultimo evento: %s\n" "$BASE" "${LAST_ID:-nenhum}"
  cat <<'EOF'
  1) Saude do servico (healthz / readyz)
  2) Listar eventos
  3) Criar evento
  4) Buscar evento por ID
  5) Publicar evento
  6) Cancelar evento
  7) Remover evento
  8) Consultar CEP (API externa ViaCEP)
  9) Mostrar erros de negocio (400 / 404 / 422)
  0) Sair
EOF
}

while true; do
  menu
  read -r -p "Opcao: " opt
  case "$opt" in
    1) health ;;
    2) list_events ;;
    3) create_event ;;
    4) get_event ;;
    5) publish_event ;;
    6) cancel_event ;;
    7) delete_event ;;
    8) lookup_cep ;;
    9) show_errors ;;    0|q|Q) printf "Ate mais.\n"; exit 0 ;;
    *) printf "%sOpcao invalida.%s\n" "$YELLOW" "$RESET" ;;
  esac
  printf "\n"
  read -r -p "Enter para voltar ao menu..." _
done
