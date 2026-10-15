package domain

import (
	"errors"
	"strings"
	"time"
)

// Erros de dominio. A camada REST traduz cada um para um status HTTP.
var (
	ErrNotFound          = errors.New("evento nao encontrado")
	ErrInvalidTransition = errors.New("transicao de status invalida")
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusCancelled Status = "cancelled"
)

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPublished, StatusCancelled:
		return true
	}
	return false
}

// Event e a entidade central deste microsservico. O servico de ingressos
// (fase 2) vai consultar capacidade e status daqui via gRPC.
type Event struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Venue       string    `json:"venue"`
	City        string    `json:"city"`
	StartsAt    time.Time `json:"starts_at"`
	Capacity    int32     `json:"capacity"`
	PriceCents  int64     `json:"price_cents"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CanPublish garante que so eventos em rascunho e com data futura virem
// publicos.
func (e *Event) CanPublish() error {
	if e.Status == StatusCancelled {
		return ErrInvalidTransition
	}
	if e.Status == StatusPublished {
		return nil // idempotente
	}
	if e.StartsAt.Before(time.Now()) {
		return errors.New("nao e possivel publicar evento com data no passado")
	}
	return nil
}

// CanCancel permite cancelar de qualquer estado; cancelar de novo e idempotente.
func (e *Event) CanCancel() error {
	return nil
}

// ValidationError acumula os problemas encontrados no payload para que o
// cliente receba todos de uma vez, e nao um por requisicao.
type ValidationError struct {
	Fields map[string]string
}

func (v *ValidationError) Error() string {
	keys := make([]string, 0, len(v.Fields))
	for k := range v.Fields {
		keys = append(keys, k)
	}
	return "payload invalido: " + strings.Join(keys, ", ")
}

func (v *ValidationError) Add(field, msg string) {
	if v.Fields == nil {
		v.Fields = map[string]string{}
	}
	v.Fields[field] = msg
}

func (v *ValidationError) HasErrors() bool { return len(v.Fields) > 0 }

// EventInput e o payload aceito em POST e PUT.
type EventInput struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Venue       string    `json:"venue"`
	City        string    `json:"city"`
	StartsAt    time.Time `json:"starts_at"`
	Capacity    int32     `json:"capacity"`
	PriceCents  int64     `json:"price_cents"`
}

func (in *EventInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Venue = strings.TrimSpace(in.Venue)
	in.City = strings.TrimSpace(in.City)
}

func (in EventInput) Validate() error {
	v := &ValidationError{}

	if in.Name == "" {
		v.Add("name", "obrigatorio")
	} else if len(in.Name) > 160 {
		v.Add("name", "maximo de 160 caracteres")
	}
	if len(in.Description) > 2000 {
		v.Add("description", "maximo de 2000 caracteres")
	}
	if in.Venue == "" {
		v.Add("venue", "obrigatorio")
	}
	if in.City == "" {
		v.Add("city", "obrigatorio")
	}
	if in.StartsAt.IsZero() {
		v.Add("starts_at", "obrigatorio, no formato RFC3339 (ex: 2026-11-20T20:00:00Z)")
	} else if in.StartsAt.Before(time.Now()) {
		v.Add("starts_at", "deve ser uma data futura")
	}
	if in.Capacity <= 0 {
		v.Add("capacity", "deve ser maior que zero")
	}
	if in.PriceCents < 0 {
		v.Add("price_cents", "nao pode ser negativo")
	}

	if v.HasErrors() {
		return v
	}
	return nil
}

// ListFilter controla paginacao e filtros do GET /events.
type ListFilter struct {
	City   string
	Status Status
	Limit  int
	Offset int
}

// Page e o envelope de resposta das listagens.
type Page struct {
	Items  []Event `json:"items"`
	Total  int64   `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}
