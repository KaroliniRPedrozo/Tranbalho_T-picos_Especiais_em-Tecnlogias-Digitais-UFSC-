package rest

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

// ErrorBody e o envelope unico de erro da API. Um formato so facilita a vida
// de quem consome e dos outros servicos na fase 2.
type ErrorBody struct {
	Error struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		Fields    map[string]string `json:"fields,omitempty"`
		RequestID string            `json:"request_id,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		observability.LoggerFrom(r.Context()).Error("falha ao escrever resposta",
			slog.String("erro", err.Error()))
	}
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	var body ErrorBody
	body.Error.Code = code
	body.Error.Message = message
	body.Error.Fields = fields
	body.Error.RequestID = observability.RequestIDFrom(r.Context())

	writeJSON(w, r, status, body)
}
