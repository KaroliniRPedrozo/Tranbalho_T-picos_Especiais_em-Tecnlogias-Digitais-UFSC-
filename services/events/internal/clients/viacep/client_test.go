package viacep

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCEP(t *testing.T) {
	got, err := NormalizeCEP("88900-000")
	require.NoError(t, err)
	assert.Equal(t, "88900000", got)

	for _, bad := range []string{"", "123", "abcdefgh", "123456789"} {
		_, err := NormalizeCEP(bad)
		assert.ErrorIs(t, err, ErrInvalidCEP, bad)
	}
}

func newServer(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, 500*time.Millisecond)
}

func TestLookup_Sucesso(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/ws/88900000/json/", r.URL.Path)
		w.Write([]byte(`{"cep":"88900-000","logradouro":"Rua X","bairro":"Centro","localidade":"Araranguá","uf":"SC"}`))
	})

	addr, err := c.Lookup(context.Background(), "88900-000")
	require.NoError(t, err)
	assert.Equal(t, "Araranguá", addr.City)
	assert.Equal(t, "SC", addr.State)
}

func TestLookup_ErrosDeNegocio(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"erro":"true"}`))
	})
	_, err := c.Lookup(context.Background(), "00000000")
	assert.ErrorIs(t, err, ErrCEPNotFound)

	_, err = c.Lookup(context.Background(), "12")
	assert.ErrorIs(t, err, ErrInvalidCEP)

	c = newServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadRequest) })
	_, err = c.Lookup(context.Background(), "88900000")
	assert.ErrorIs(t, err, ErrInvalidCEP)
}

func TestLookup_ErrosDeInfra(t *testing.T) {
	t.Run("status 500", func(t *testing.T) {
		c := newServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		_, err := c.Lookup(context.Background(), "88900000")
		assert.ErrorIs(t, err, ErrUnavailable)
	})
	t.Run("json invalido", func(t *testing.T) {
		c := newServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) })
		_, err := c.Lookup(context.Background(), "88900000")
		assert.ErrorIs(t, err, ErrUnavailable)
	})
	t.Run("timeout", func(t *testing.T) {
		c := newServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) })
		_, err := c.Lookup(context.Background(), "88900000")
		assert.ErrorIs(t, err, ErrUnavailable)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
	t.Run("contexto cancelado", func(t *testing.T) {
		c := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Lookup(ctx, "88900000")
		assert.ErrorIs(t, err, ErrUnavailable)
	})
	t.Run("servico fora do ar", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, err := New(url, time.Second).Lookup(context.Background(), "88900000")
		assert.ErrorIs(t, err, ErrUnavailable)
	})
}
