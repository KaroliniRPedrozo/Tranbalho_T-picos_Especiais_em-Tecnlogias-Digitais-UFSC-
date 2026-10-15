package rest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/clients/viacep"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

// AddressLookup e a dependencia externa do handler; o client do ViaCEP a
// satisfaz e os testes usam um fake.
type AddressLookup interface {
	Lookup(ctx context.Context, cep string) (viacep.Address, error)
}

type AddressHandler struct {
	lookup AddressLookup
}

func NewAddressHandler(l AddressLookup) *AddressHandler {
	return &AddressHandler{lookup: l}
}

// Get consulta o endereco de um CEP. Erro de negocio vira 4xx; falha do
// servico externo vira 502 (ou 504 em timeout), nunca 500.
func (h *AddressHandler) Get(w http.ResponseWriter, r *http.Request) {
	// Pega o CEP da URL (/addresses/{cep}) e delega a consulta ao client.
	addr, err := h.lookup.Lookup(r.Context(), chi.URLParam(r, "cep"))
	switch {
	// Sucesso: 200 com o endereco em JSON.
	case err == nil:
		writeJSON(w, r, http.StatusOK, addr)
	// Erro de negocio: CEP mal formado -> 400.
	case errors.Is(err, viacep.ErrInvalidCEP):
		writeError(w, r, http.StatusBadRequest, "invalid_cep", err.Error(), nil)
	// Erro de negocio: CEP nao existe -> 404.
	case errors.Is(err, viacep.ErrCEPNotFound):
		writeError(w, r, http.StatusNotFound, "cep_not_found", err.Error(), nil)
	// Erro de infraestrutura: ViaCEP fora do ar ou lento. Registra no log
	// e responde 502 (ou 504 se foi timeout), sem expor detalhes ao cliente.
	case errors.Is(err, viacep.ErrUnavailable):
		observability.LoggerFrom(r.Context()).Error("servico de cep falhou", slog.String("erro", err.Error()))
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, r, status, "external_service_unavailable", "servico de cep indisponivel", nil)
	// Qualquer outro erro inesperado -> 500.
	default:
		observability.LoggerFrom(r.Context()).Error("erro nao tratado", slog.String("erro", err.Error()))
		writeError(w, r, http.StatusInternalServerError, "internal_error", "erro interno inesperado", nil)
	}
}
