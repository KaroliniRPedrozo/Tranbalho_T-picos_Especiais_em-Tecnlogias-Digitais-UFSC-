package rest

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

// ErrorBody e o envelope unico de erro da API. Um formato so facilita a vida
// de quem consome e dos outros servicos na fase 2.
//
// Exemplo do que o cliente recebe num erro 422:
//
//	{"error": {"code": "validation_error", "message": "payload invalido",
//	           "fields": {"name": "obrigatorio"}, "request_id": "a1b2c3..."}}
//
// code    -> texto estavel para programas tratarem (nao muda com a traducao)
// message -> texto para humanos
// fields  -> so aparece em erro de validacao (omitempty some quando vazio)
// request_id -> permite achar a mesma requisicao nos logs
type ErrorBody struct {
	Error struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		Fields    map[string]string `json:"fields,omitempty"`
		RequestID string            `json:"request_id,omitempty"`
	} `json:"error"`
}

// writeJSON escreve o status e o corpo em JSON; payload nil envia so o status.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	// Ordem obrigatoria no net/http: primeiro os cabecalhos, depois o status,
	// depois o corpo. Cabecalho definido apos WriteHeader e ignorado.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	// Respostas sem corpo (ex: 204) passam nil.
	if payload == nil {
		return
	}
	// Encode transforma a struct Go em JSON (usando as tags `json:"..."`).
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		observability.LoggerFrom(r.Context()).Error("falha ao escrever resposta",
			slog.String("erro", err.Error()))
	}
}

// writeError monta o envelope de erro padrao, incluindo o request_id.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	// Todo erro da API passa por aqui, entao todos tem o mesmo formato.
	var body ErrorBody
	body.Error.Code = code
	body.Error.Message = message
	body.Error.Fields = fields
	body.Error.RequestID = observability.RequestIDFrom(r.Context())

	writeJSON(w, r, status, body)
}
