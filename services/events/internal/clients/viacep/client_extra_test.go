package viacep

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_RemoveBarraFinalDaURL(t *testing.T) {
	c := New("https://viacep.com.br///", time.Second)
	assert.Equal(t, "https://viacep.com.br", c.baseURL)
}

func TestNormalizeCEP_AceitaMascaraEEspacos(t *testing.T) {
	for _, entrada := range []string{"88900-000", "88900000", " 88900-000 ", "88.900-000"} {
		got, err := NormalizeCEP(entrada)
		require.NoError(t, err, entrada)
		assert.Equal(t, "88900000", got, entrada)
	}
}

func TestLookup_ErroBooleanoEhCEPInexistente(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"erro":true}`)) })
	_, err := c.Lookup(context.Background(), "00000000")
	assert.ErrorIs(t, err, ErrCEPNotFound)
}

func TestLookup_ErroFalsoNaoBloqueia(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"erro":false,"cep":"88900-000","localidade":"Araranguá","uf":"SC"}`))
	})
	addr, err := c.Lookup(context.Background(), "88900000")
	require.NoError(t, err)
	assert.Equal(t, "Araranguá", addr.City)
}

func TestLookup_Status404EhCEPInexistente(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, err := c.Lookup(context.Background(), "88900000")
	assert.ErrorIs(t, err, ErrCEPNotFound)
}

func TestLookup_TraduzTodosOsCampos(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cep":"01001-000","logradouro":"Praça da Sé","bairro":"Sé","localidade":"São Paulo","uf":"SP"}`))
	})
	addr, err := c.Lookup(context.Background(), "01001000")
	require.NoError(t, err)

	assert.Equal(t, Address{
		CEP: "01001-000", Street: "Praça da Sé", Neighborhood: "Sé", City: "São Paulo", State: "SP",
	}, addr)
}

func TestLookup_CEPInvalidoNaoChamaAAPIExterna(t *testing.T) {
	chamadas := 0
	c := newServer(t, func(http.ResponseWriter, *http.Request) { chamadas++ })

	_, err := c.Lookup(context.Background(), "123")
	assert.ErrorIs(t, err, ErrInvalidCEP)
	assert.Zero(t, chamadas, "validacao local deve evitar a chamada de rede")
}
