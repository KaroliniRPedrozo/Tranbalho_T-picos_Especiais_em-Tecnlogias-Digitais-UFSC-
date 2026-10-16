package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/clients/viacep"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/domain"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

const validID = "11111111-1111-1111-1111-111111111111"

// fakeStore implementa EventStore em memoria para os testes de handler.
// Como o handler depende de uma INTERFACE (e nao do Postgres), conseguimos
// testar toda a camada HTTP sem banco: o campo err simula uma falha e o campo
// event e o que o "banco" devolve.
type fakeStore struct {
	event domain.Event
	err   error
}

func (f *fakeStore) Create(_ context.Context, in domain.EventInput) (domain.Event, error) {
	return domain.Event{ID: validID, Name: in.Name, Status: domain.StatusDraft}, f.err
}
func (f *fakeStore) GetByID(context.Context, string) (domain.Event, error) { return f.event, f.err }
func (f *fakeStore) List(context.Context, domain.ListFilter) (domain.Page, error) {
	return domain.Page{Items: []domain.Event{}}, f.err
}
func (f *fakeStore) Update(context.Context, string, domain.EventInput) (domain.Event, error) {
	return f.event, f.err
}
func (f *fakeStore) UpdateStatus(_ context.Context, _ string, s domain.Status) (domain.Event, error) {
	e := f.event
	e.Status = s
	return e, f.err
}
func (f *fakeStore) Delete(context.Context, string) error { return f.err }

type fakeLookup struct {
	addr viacep.Address
	err  error
}

func (f fakeLookup) Lookup(context.Context, string) (viacep.Address, error) { return f.addr, f.err }

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

// do monta o roteador real com as dependencias falsas e dispara uma requisicao
// HTTP "de mentira" (httptest), sem abrir porta de rede. Devolve o recorder
// para os testes conferirem status, cabecalhos e corpo.
func do(t *testing.T, store *fakeStore, lookup AddressLookup, method, path, body string) *httptest.ResponseRecorder {
	t.Helper() // em caso de falha, aponta a linha do teste e nao a deste helper
	if lookup == nil {
		lookup = fakeLookup{}
	}
	router := NewRouter(RouterDeps{
		Logger:     observability.NewLogger("test", "test", "error"),
		EventStore: store,
		Addresses:  lookup,
		Pinger:     fakePinger{},
	})
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	return b.Error.Code
}

func validBody() string {
	return `{"name":"Show","venue":"Teatro","city":"Araranguá","starts_at":"` +
		time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339) + `","capacity":100,"price_cents":5000}`
}

// TestCreate cobre os caminhos do POST /events: o feliz (201) e cada tipo de
// erro, mostrando a diferenca entre 400 (formato), 422 (regra) e 500 (infra).
// t.Run cria um sub-teste por cenario, que aparece separado na saida.
func TestCreate(t *testing.T) {
	t.Run("201 com Location", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPost, "/api/v1/events", validBody())
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, "/api/v1/events/"+validID, rec.Header().Get("Location"))
	})
	t.Run("400 JSON malformado", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPost, "/api/v1/events", `{nope`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_body", errorCode(t, rec))
	})
	t.Run("400 corpo vazio", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPost, "/api/v1/events", ``)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("400 campo desconhecido", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPost, "/api/v1/events", `{"x":1}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("422 validacao de negocio", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPost, "/api/v1/events", `{"name":""}`)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, "validation_error", errorCode(t, rec))
	})
	t.Run("500 erro de infra nao vaza detalhe", func(t *testing.T) {
		rec := do(t, &fakeStore{err: assert.AnError}, nil, http.MethodPost, "/api/v1/events", validBody())
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.False(t, strings.Contains(rec.Body.String(), assert.AnError.Error()))
	})
}

func TestGet(t *testing.T) {
	t.Run("200", func(t *testing.T) {
		rec := do(t, &fakeStore{event: domain.Event{ID: validID}}, nil, http.MethodGet, "/api/v1/events/"+validID, "")
		assert.Equal(t, http.StatusOK, rec.Code)
	})
	t.Run("404 nao encontrado", func(t *testing.T) {
		rec := do(t, &fakeStore{err: domain.ErrNotFound}, nil, http.MethodGet, "/api/v1/events/"+validID, "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "not_found", errorCode(t, rec))
	})
	t.Run("400 id invalido", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodGet, "/api/v1/events/abc", "")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("500 infra distinto de 404", func(t *testing.T) {
		rec := do(t, &fakeStore{err: assert.AnError}, nil, http.MethodGet, "/api/v1/events/"+validID, "")
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestList_QueryInvalida(t *testing.T) {
	for _, q := range []string{"?status=x", "?limit=0", "?limit=abc", "?offset=-1"} {
		rec := do(t, &fakeStore{}, nil, http.MethodGet, "/api/v1/events"+q, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
	}
	rec := do(t, &fakeStore{}, nil, http.MethodGet, "/api/v1/events?limit=10&status=draft", "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDelete(t *testing.T) {
	assert.Equal(t, http.StatusNoContent,
		do(t, &fakeStore{}, nil, http.MethodDelete, "/api/v1/events/"+validID, "").Code)
	assert.Equal(t, http.StatusNotFound,
		do(t, &fakeStore{err: domain.ErrNotFound}, nil, http.MethodDelete, "/api/v1/events/"+validID, "").Code)
}

func TestTransitions(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)

	rec := do(t, &fakeStore{event: domain.Event{Status: domain.StatusDraft, StartsAt: future}},
		nil, http.MethodPost, "/api/v1/events/"+validID+"/publish", "")
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = do(t, &fakeStore{event: domain.Event{Status: domain.StatusCancelled, StartsAt: future}},
		nil, http.MethodPost, "/api/v1/events/"+validID+"/publish", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "invalid_transition", errorCode(t, rec))

	rec = do(t, &fakeStore{event: domain.Event{Status: domain.StatusPublished}},
		nil, http.MethodPost, "/api/v1/events/"+validID+"/cancel", "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAddress e um teste "orientado a tabela" (table-driven), o padrao do Go:
// cada linha da tabela e um cenario (erro simulado -> status esperado), e o
// mesmo laco roda todos. Para testar um caso novo basta adicionar uma linha.
func TestAddress(t *testing.T) {
	cases := []struct {
		nome   string
		err    error
		status int
	}{
		{"sucesso", nil, http.StatusOK},
		{"cep invalido", viacep.ErrInvalidCEP, http.StatusBadRequest},
		{"cep inexistente", viacep.ErrCEPNotFound, http.StatusNotFound},
		{"servico fora do ar", viacep.ErrUnavailable, http.StatusBadGateway},
		{"erro inesperado", assert.AnError, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			rec := do(t, &fakeStore{}, fakeLookup{err: c.err}, http.MethodGet, "/api/v1/addresses/88900000", "")
			assert.Equal(t, c.status, rec.Code)
		})
	}

	t.Run("timeout externo vira 504", func(t *testing.T) {
		err := &wrapErr{viacep.ErrUnavailable, context.DeadlineExceeded}
		rec := do(t, &fakeStore{}, fakeLookup{err: err}, http.MethodGet, "/api/v1/addresses/88900000", "")
		assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
	})
}

// wrapErr combina duas causas, como fmt.Errorf("%w: %w").
type wrapErr struct{ a, b error }

func (w *wrapErr) Error() string   { return w.a.Error() + ": " + w.b.Error() }
func (w *wrapErr) Unwrap() []error { return []error{w.a, w.b} }

func TestHealth(t *testing.T) {
	rec := do(t, &fakeStore{}, nil, http.MethodGet, "/healthz", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, http.StatusNotFound, do(t, &fakeStore{}, nil, http.MethodGet, "/nada", "").Code)
}
