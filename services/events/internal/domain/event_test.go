package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestEvent_CanPublish testa a regra de negócio de transição de status e datas do evento.
func TestEvent_CanPublish(t *testing.T) {
	casos := []struct {
		nome     string
		evento   Event
		esperado error
	}{
		{
			nome: "rascunho com data futura pode ser publicado",
			evento: Event{
				Status:   StatusDraft,
				StartsAt: time.Now().Add(24 * time.Hour), // Data no futuro
			},
			esperado: nil,
		},
		{
			nome: "tentar publicar evento ja publicado deve retornar nil (idempotente)",
			evento: Event{
				Status:   StatusPublished,
				StartsAt: time.Now().Add(24 * time.Hour),
			},
			esperado: nil,
		},
		{
			nome: "evento cancelado nao pode ser publicado",
			evento: Event{
				Status: StatusCancelled,
			},
			esperado: ErrInvalidTransition,
		},
		{
			nome: "rascunho com data no passado nao pode ser publicado",
			evento: Event{
				Status:   StatusDraft,
				StartsAt: time.Now().Add(-24 * time.Hour), // Data no passado (ontem)
			},
			esperado: errors.New("nao e possivel publicar evento com data no passado"),
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := c.evento.CanPublish()

			if c.esperado == nil {
				assert.NoError(t, err) // Garante que não houve erro
			} else if c.esperado == ErrInvalidTransition {
				assert.ErrorIs(t, err, c.esperado) // Compara erros específicos
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "passado") // Verifica parte da mensagem
			}
		})
	}
}

// TestEventInput_Validate testa o payload de entrada (rejeição de dados ruins).
func TestEventInput_Validate(t *testing.T) {
	casos := []struct {
		nome       string
		input      EventInput
		esperaErro bool
	}{
		{
			nome: "payload perfeitamente valido",
			input: EventInput{
				Name:       "Show de Rock",
				Venue:      "Estadio Municipal",
				City:       "Araranguá",
				StartsAt:   time.Now().Add(48 * time.Hour),
				Capacity:   5000,
				PriceCents: 15000, // R$ 150,00
			},
			esperaErro: false,
		},
		{
			nome: "falha por campos em branco",
			input: EventInput{
				Name:       "", // Obrigatorio
				Venue:      "", // Obrigatorio
				City:       "", // Obrigatorio
				StartsAt:   time.Now().Add(48 * time.Hour),
				Capacity:   100,
				PriceCents: 5000,
			},
			esperaErro: true,
		},
		{
			nome: "falha por regras de negocio (capacidade zero e preco negativo)",
			input: EventInput{
				Name:       "Show",
				Venue:      "Teatro",
				City:       "Criciúma",
				StartsAt:   time.Now().Add(48 * time.Hour),
				Capacity:   0,   // Deve ser maior que zero
				PriceCents: -50, // Nao pode ser negativo
			},
			esperaErro: true,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := c.input.Validate()

			if c.esperaErro {
				assert.Error(t, err)
				assert.IsType(t, &ValidationError{}, err) // Garante que o erro retornado é do tipo ValidationError
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEvent_CanCancel(t *testing.T) {
	for _, s := range []Status{StatusDraft, StatusPublished, StatusCancelled} {
		e := Event{Status: s}
		assert.NoError(t, e.CanCancel(), string(s))
	}
}

func TestStatus_Valid(t *testing.T) {
	assert.True(t, StatusDraft.Valid())
	assert.False(t, Status("outro").Valid())
}
