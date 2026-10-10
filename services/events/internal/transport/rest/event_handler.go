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

	"github.com/seuusuario/ticketing/services/events/internal/domain"
	"github.com/seuusuario/ticketing/services/events/internal/observability"
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

type EventHandler struct {
	store EventStore
}

func NewEventHandler(store EventStore) *EventHandler {
	return &EventHandler{store: store}
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	defaultLimit = 20
	maxLimit     = 100
	maxBodyBytes = 1 << 20 // 1 MiB
)

func (h *EventHandler) Create(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeInput(w, r)
	if !ok {
		return
	}

	event, err := h.store.Create(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	observability.LoggerFrom(r.Context()).Info("evento criado",
		slog.String("event_id", event.ID),
		slog.String("event_name", event.Name),
	)

	w.Header().Set("Location", "/api/v1/events/"+event.ID)
	writeJSON(w, r, http.StatusCreated, event)
}

func (h *EventHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := domain.ListFilter{
		City:   q.Get("city"),
		Limit:  defaultLimit,
		Offset: 0,
	}

	if s := q.Get("status"); s != "" {
		status := domain.Status(s)
		if !status.Valid() {
			writeError(w, r, http.StatusBadRequest, "invalid_query",
				"status deve ser draft, published ou cancelled", nil)
			return
		}
		filter.Status = status
	}

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

func (h *EventHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := eventID(w, r)
	if !ok {
		return
	}

	event, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, event)
}

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

func (h *EventHandler) transition(w http.ResponseWriter, r *http.Request, target domain.Status) {
	id, ok := eventID(w, r)
	if !ok {
		return
	}

	current, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	var ruleErr error
	switch target {
	case domain.StatusPublished:
		ruleErr = current.CanPublish()
	case domain.StatusCancelled:
		ruleErr = current.CanCancel()
	}
	if ruleErr != nil {
		writeError(w, r, http.StatusConflict, "invalid_transition", ruleErr.Error(), nil)
		return
	}

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
	w.WriteHeader(http.StatusNoContent)
}

// fail traduz erro de dominio em status HTTP. Erro inesperado vira 500 e so o
// log guarda o detalhe tecnico, para nao vazar interno na resposta.
func (h *EventHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
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

func eventID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !uuidRe.MatchString(id) {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "id deve ser um UUID valido", nil)
		return "", false
	}
	return id, true
}

func decodeInput(w http.ResponseWriter, r *http.Request) (domain.EventInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var in domain.EventInput
	if err := dec.Decode(&in); err != nil {
		msg := "corpo da requisicao invalido: " + err.Error()
		if errors.Is(err, io.EOF) {
			msg = "corpo da requisicao vazio"
		}
		writeError(w, r, http.StatusBadRequest, "invalid_body", msg, nil)
		return domain.EventInput{}, false
	}

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
