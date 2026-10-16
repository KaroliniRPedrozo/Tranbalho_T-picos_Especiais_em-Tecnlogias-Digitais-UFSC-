package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/domain"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

// EventStore e a dependencia que o handler precisa. Declarar a interface aqui
// (no consumidor) permite trocar o Postgres por um fake nos testes.
type EventStore interface {
	Create(ctx context.Context, in domain.EventInput) (domain.Event, error)
	GetByID(ctx context.Context, id string) (domain.Event, error)
	List(ctx context.Context, f domain.ListFilter) (domain.Page, error)
	Update(ctx context.Context, id string, in domain.EventInput) (domain.Event, error)
	UpdateStatus(ctx context.Context, id string, status domain.Status) (domain.Event, error)
	Delete(ctx context.Context, id string) error
}

// EventHandler agrupa os handlers HTTP de /api/v1/events.
type EventHandler struct {
	store EventStore
}

// NewEventHandler cria o handler recebendo o repositorio por interface.
func NewEventHandler(store EventStore) *EventHandler {
	return &EventHandler{store: store}
}

// uuidRe valida o formato do id na URL antes de ir ao banco.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	defaultLimit = 20
	maxLimit     = 100
	maxBodyBytes = 1 << 20 // 1 MiB
)

// Create trata POST /events: valida o corpo, grava e responde 201 + Location.
func (h *EventHandler) Create(w http.ResponseWriter, r *http.Request) {
	// 1. Le e valida o JSON. Se estiver errado, decodeInput ja respondeu
	// (400 ou 422) e aqui so paramos (padrao "return cedo").
	in, ok := decodeInput(w, r)
	if !ok {
		return
	}

	// 2. Grava no banco. r.Context() leva o cancelamento: se o cliente
	// desistir da requisicao, a consulta ao banco tambem e cancelada.
	event, err := h.store.Create(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// 3. Log de negocio (alem do log de acesso do middleware).
	observability.LoggerFrom(r.Context()).Info("evento criado",
		slog.String("event_id", event.ID),
		slog.String("event_name", event.Name),
	)

	// 4. 201 Created + cabecalho Location com o endereco do novo recurso:
	// e o que a convencao REST pede para criacao.
	w.Header().Set("Location", "/api/v1/events/"+event.ID)
	writeJSON(w, r, http.StatusCreated, event)
}

// List trata GET /events com filtros opcionais (city, status, limit, offset).
func (h *EventHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := domain.ListFilter{
		City:   q.Get("city"),
		Limit:  defaultLimit,
		Offset: 0,
	}

	// status: so aceita draft, published ou cancelled.
	if s := q.Get("status"); s != "" {
		status := domain.Status(s)
		if !status.Valid() {
			writeError(w, r, http.StatusBadRequest, "invalid_query",
				"status deve ser draft, published ou cancelled", nil)
			return
		}
		filter.Status = status
	}

	// limit: inteiro positivo, limitado a maxLimit.
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, r, http.StatusBadRequest, "invalid_query", "limit deve ser um inteiro positivo", nil)
			return
		}
		if n > maxLimit {
			n = maxLimit
		}
		filter.Limit = n
	}

	// offset: inteiro nao negativo.
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, r, http.StatusBadRequest, "invalid_query", "offset deve ser um inteiro nao negativo", nil)
			return
		}
		filter.Offset = n
	}

	page, err := h.store.List(r.Context(), filter)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, page)
}

// Get trata GET /events/{id}.
func (h *EventHandler) Get(w http.ResponseWriter, r *http.Request) {
	// Valida o id antes de ir ao banco: id mal formado e erro do cliente (400),
	// nao precisa gastar uma consulta para descobrir.
	id, ok := eventID(w, r)
	if !ok {
		return
	}

	// ErrNotFound vira 404 dentro de h.fail.
	event, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, event)
}

// Update trata PUT /events/{id}: valida o id, valida o corpo e atualiza.
func (h *EventHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := eventID(w, r)
	if !ok {
		return
	}
	in, ok := decodeInput(w, r)
	if !ok {
		return
	}

	event, err := h.store.Update(r.Context(), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	observability.LoggerFrom(r.Context()).Info("evento atualizado", slog.String("event_id", id))
	writeJSON(w, r, http.StatusOK, event)
}

// Publish e Cancel sao transicoes de estado explicitas, em vez de um PATCH
// generico de status. Fica mais claro o que a API permite.
func (h *EventHandler) Publish(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, domain.StatusPublished)
}

func (h *EventHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, domain.StatusCancelled)
}

// transition aplica a regra de negocio (CanPublish/CanCancel) e, se permitida,
// grava o novo status. Regra violada vira 409.
func (h *EventHandler) transition(w http.ResponseWriter, r *http.Request, target domain.Status) {
	id, ok := eventID(w, r)
	if !ok {
		return
	}

	// 1. Busca o estado atual: a regra depende dele (ex: cancelado nao publica).
	current, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// 2. Pergunta ao dominio se a transicao e permitida. A regra mora no
	// domain (Event.CanPublish/CanCancel), nao aqui: o handler so a aplica.
	var ruleErr error
	switch target {
	case domain.StatusPublished:
		ruleErr = current.CanPublish()
	case domain.StatusCancelled:
		ruleErr = current.CanCancel()
	}
	// 409 Conflict: o pedido e valido, mas conflita com o estado atual do recurso.
	if ruleErr != nil {
		writeError(w, r, http.StatusConflict, "invalid_transition", ruleErr.Error(), nil)
		return
	}

	// 3. Permitido: grava o novo status.
	event, err := h.store.UpdateStatus(r.Context(), id, target)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	observability.LoggerFrom(r.Context()).Info("status do evento alterado",
		slog.String("event_id", id),
		slog.String("de", string(current.Status)),
		slog.String("para", string(target)),
	)

	writeJSON(w, r, http.StatusOK, event)
}

// Delete trata DELETE /events/{id} e responde 204 sem corpo.
func (h *EventHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := eventID(w, r)
	if !ok {
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}

	observability.LoggerFrom(r.Context()).Info("evento removido", slog.String("event_id", id))
	// 204 No Content: deu certo e nao ha nada a devolver no corpo.
	w.WriteHeader(http.StatusNoContent)
}

// fail traduz erro de dominio em status HTTP. Erro inesperado vira 500 e so o
// log guarda o detalhe tecnico, para nao vazar interno na resposta.
func (h *EventHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	// errors.Is percorre a cadeia de erros embrulhados (fmt.Errorf("%w")) e
	// procura o erro de dominio. E o ponto central da traducao
	// "erro de negocio -> status HTTP".
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "evento nao encontrado", nil)
	case errors.Is(err, domain.ErrInvalidTransition):
		writeError(w, r, http.StatusConflict, "invalid_transition", err.Error(), nil)
	default:
		observability.LoggerFrom(r.Context()).Error("erro nao tratado", slog.String("erro", err.Error()))
		writeError(w, r, http.StatusInternalServerError, "internal_error", "erro interno inesperado", nil)
	}
}

// eventID le o {id} da URL e rejeita com 400 se nao for um UUID.
func eventID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !uuidRe.MatchString(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "id deve ser um UUID valido", nil)
		return "", false
	}
	return id, true
}

// decodeInput le e valida o JSON do corpo. Em caso de erro ja responde ao
// cliente (400 ou 422) e devolve ok=false.
func decodeInput(w http.ResponseWriter, r *http.Request) (domain.EventInput, bool) {
	// Limita o tamanho do corpo (protege contra envio de arquivos enormes) e
	// recusa campos desconhecidos (pega erro de digitacao do cliente, como
	// "nmae" em vez de "name", em vez de ignorar em silencio).
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	// Decode preenche a struct a partir do JSON. Falhou = 400 (formato errado).
	// A diferenca para o 422 mais abaixo: aqui o JSON nem e legivel; la ele e
	// legivel, mas viola uma regra de negocio.
	var in domain.EventInput
	if err := dec.Decode(&in); err != nil {
		msg := "corpo da requisicao invalido: " + err.Error()
		if errors.Is(err, io.EOF) {
			msg = "corpo da requisicao vazio"
		}
		writeError(w, r, http.StatusBadRequest, "invalid_body", msg, nil)
		return domain.EventInput{}, false
	}

	// Limpa os textos e aplica as regras de negocio (422 se reprovar).
	in.Normalize()

	if err := in.Validate(); err != nil {
		var ve *domain.ValidationError
		if errors.As(err, &ve) {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "payload invalido", ve.Fields)
			return domain.EventInput{}, false
		}
		writeError(w, r, http.StatusUnprocessableEntity, "validation_error", err.Error(), nil)
		return domain.EventInput{}, false
	}

	return in, true
}
