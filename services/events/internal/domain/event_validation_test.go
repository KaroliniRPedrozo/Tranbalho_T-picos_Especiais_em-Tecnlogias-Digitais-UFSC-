package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validInput devolve um payload valido para os testes alterarem um campo por vez.
func validInput() EventInput {
	return EventInput{
		Name:       "Show",
		Venue:      "Teatro",
		City:       "Araranguá",
		StartsAt:   time.Now().Add(48 * time.Hour),
		Capacity:   10,
		PriceCents: 0,
	}
}

func TestEventInput_Normalize(t *testing.T) {
	in := EventInput{Name: "  Show  ", Description: "\tdesc\n", Venue: " Teatro ", City: " Criciúma "}
	in.Normalize()

	assert.Equal(t, "Show", in.Name)
	assert.Equal(t, "desc", in.Description)
	assert.Equal(t, "Teatro", in.Venue)
	assert.Equal(t, "Criciúma", in.City)
}

func TestEventInput_Validate_NomeSoComEspacosEhInvalido(t *testing.T) {
	in := validInput()
	in.Name = "    "
	in.Normalize()

	err := in.Validate()
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Contains(t, ve.Fields, "name")
}

func TestEventInput_Validate_Limites(t *testing.T) {
	casos := []struct {
		nome    string
		mutar   func(*EventInput)
		campo   string
		invalid bool
	}{
		{"nome com 160 caracteres e aceito", func(in *EventInput) { in.Name = strings.Repeat("a", 160) }, "name", false},
		{"nome com 161 caracteres e recusado", func(in *EventInput) { in.Name = strings.Repeat("a", 161) }, "name", true},
		{"descricao com 2000 caracteres e aceita", func(in *EventInput) { in.Description = strings.Repeat("a", 2000) }, "description", false},
		{"descricao com 2001 caracteres e recusada", func(in *EventInput) { in.Description = strings.Repeat("a", 2001) }, "description", true},
		{"capacidade 1 e aceita", func(in *EventInput) { in.Capacity = 1 }, "capacity", false},
		{"capacidade 0 e recusada", func(in *EventInput) { in.Capacity = 0 }, "capacity", true},
		{"capacidade negativa e recusada", func(in *EventInput) { in.Capacity = -5 }, "capacity", true},
		{"preco zero (gratuito) e aceito", func(in *EventInput) { in.PriceCents = 0 }, "price_cents", false},
		{"preco negativo e recusado", func(in *EventInput) { in.PriceCents = -1 }, "price_cents", true},
		{"data no passado e recusada", func(in *EventInput) { in.StartsAt = time.Now().Add(-time.Hour) }, "starts_at", true},
		{"data zerada e recusada", func(in *EventInput) { in.StartsAt = time.Time{} }, "starts_at", true},
		{"sem local e recusado", func(in *EventInput) { in.Venue = "" }, "venue", true},
		{"sem cidade e recusado", func(in *EventInput) { in.City = "" }, "city", true},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			in := validInput()
			c.mutar(&in)

			err := in.Validate()
			if !c.invalid {
				assert.NoError(t, err)
				return
			}

			var ve *ValidationError
			require.True(t, errors.As(err, &ve), "esperava *ValidationError")
			assert.Contains(t, ve.Fields, c.campo)
			assert.Len(t, ve.Fields, 1, "so o campo %s deveria falhar", c.campo)
		})
	}
}

func TestEventInput_Validate_AcumulaTodosOsErros(t *testing.T) {
	err := EventInput{}.Validate()

	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	for _, campo := range []string{"name", "venue", "city", "starts_at", "capacity"} {
		assert.Contains(t, ve.Fields, campo)
	}
}

func TestValidationError(t *testing.T) {
	v := &ValidationError{}
	assert.False(t, v.HasErrors())

	v.Add("name", "obrigatorio")
	v.Add("city", "obrigatorio")
	assert.True(t, v.HasErrors())
	assert.Len(t, v.Fields, 2)
	assert.Contains(t, v.Error(), "name")
	assert.Contains(t, v.Error(), "city")

	// Adicionar de novo no mesmo campo sobrescreve (um erro por campo).
	v.Add("name", "outro")
	assert.Len(t, v.Fields, 2)
	assert.Equal(t, "outro", v.Fields["name"])
}

func TestStatus_Valid_TodosOsValores(t *testing.T) {
	for _, s := range []Status{StatusDraft, StatusPublished, StatusCancelled} {
		assert.True(t, s.Valid(), string(s))
	}
	for _, s := range []Status{"", "DRAFT", "archived"} {
		assert.False(t, s.Valid(), string(s))
	}
}

func TestErrosDeDominio_SaoDistintos(t *testing.T) {
	assert.NotErrorIs(t, ErrNotFound, ErrInvalidTransition)
}
