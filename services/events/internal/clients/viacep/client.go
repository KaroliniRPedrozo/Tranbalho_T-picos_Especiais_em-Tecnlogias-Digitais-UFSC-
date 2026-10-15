package viacep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Erros do client. ErrInvalidCEP e ErrCEPNotFound sao erros de negocio (culpa
// do chamador); ErrUnavailable e erro de infraestrutura (servico externo fora
// do ar, timeout, resposta inesperada).
var (
	ErrInvalidCEP  = errors.New("cep invalido: informe 8 digitos")
	ErrCEPNotFound = errors.New("cep nao encontrado")
	ErrUnavailable = errors.New("servico de cep indisponivel")
)

// Address e o endereco devolvido pela consulta de CEP.
type Address struct {
	CEP          string `json:"cep"`
	Street       string `json:"street"`
	Neighborhood string `json:"neighborhood"`
	City         string `json:"city"`
	State        string `json:"state"`
}

// Client consulta o ViaCEP. O baseURL vem de configuracao para permitir
// apontar para um servidor de teste.
type Client struct {
	baseURL string
	timeout time.Duration
	http    *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		timeout: timeout,
		http:    &http.Client{},
	}
}

var nonDigits = regexp.MustCompile(`\D`)

// NormalizeCEP remove mascara ("88900-000" -> "88900000") e valida o tamanho.
func NormalizeCEP(raw string) (string, error) {
	cep := nonDigits.ReplaceAllString(raw, "")
	if len(cep) != 8 {
		return "", ErrInvalidCEP
	}
	return cep, nil
}

type response struct {
	CEP        string `json:"cep"`
	Logradouro string `json:"logradouro"`
	Bairro     string `json:"bairro"`
	Localidade string `json:"localidade"`
	UF         string `json:"uf"`
	Erro       any    `json:"erro"` // true (bool) ou "true" (string) quando nao existe
}

// Lookup consulta o CEP. O timeout vale para a chamada inteira (conexao,
// resposta e leitura do corpo) e herda o cancelamento do contexto recebido.
func (c *Client) Lookup(ctx context.Context, rawCEP string) (Address, error) {
	cep, err := NormalizeCEP(rawCEP)
	if err != nil {
		return Address{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ws/"+cep+"/json/", nil)
	if err != nil {
		return Address{}, fmt.Errorf("%w: montando requisicao: %v", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// %w duplo: preserva ErrUnavailable e a causa (ex: DeadlineExceeded).
		return Address{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusBadRequest:
		// O ViaCEP responde 400 para formato de CEP que ele rejeita.
		return Address{}, ErrInvalidCEP
	case resp.StatusCode == http.StatusNotFound:
		return Address{}, ErrCEPNotFound
	case resp.StatusCode != http.StatusOK:
		return Address{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}

	var body response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Address{}, fmt.Errorf("%w: resposta ilegivel: %v", ErrUnavailable, err)
	}
	if body.Erro != nil && body.Erro != false && body.Erro != "false" {
		return Address{}, ErrCEPNotFound
	}

	return Address{
		CEP:          body.CEP,
		Street:       body.Logradouro,
		Neighborhood: body.Bairro,
		City:         body.Localidade,
		State:        body.UF,
	}, nil
}
