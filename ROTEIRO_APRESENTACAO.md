# Roteiro de estudo — Apresentação I (events-service)

Cada seção segue os critérios do professor. Para cada um: **o que falar**,
**onde mostrar no código** e **perguntas prováveis**.

Ordem sugerida de fala (~10 min): visão geral → 1 → 2 → 3 → 4 → 5 → 6 → demo.

## 0. Abertura (1 min)

- Projeto: sistema de eventos e ingressos em microsserviços, Go 1.23.
- Esta entrega: `events-service`, REST/JSON, PostgreSQL próprio, Docker
  Compose, logs estruturados (`slog`), integração externa com o ViaCEP.
- Fase 2 (planejada): `tickets-service` e `payments-service` conversando por
  gRPC (contrato em `proto/events/v1/events.proto`), cada um com seu banco.

### Fala de abertura (pode ler quase literal)

> "Nosso projeto é um sistema de eventos e ingressos feito com microsserviços
> em Go. Hoje apresento o primeiro serviço, o **events-service**. Ele cuida do
> cadastro de eventos: criar, listar, publicar e cancelar. Ele tem API REST,
> banco PostgreSQL próprio e consulta o ViaCEP para buscar endereço pelo CEP.
> Vou mostrar a arquitetura, depois como tratamos erros e testes, e por fim
> uma demonstração no terminal."

### Como o programa funciona (para explicar com suas palavras)

É uma API HTTP, sem tela: quem chama manda uma requisição e recebe JSON.

Caminho de uma requisição, por exemplo `POST /api/v1/events`:

1. O **router** (chi) recebe a requisição. Antes do handler passam três
   middlewares: ID da requisição, log e captura de panics.
2. O **handler** lê o JSON, rejeita campos desconhecidos e limita o tamanho
   do corpo (1 MiB).
3. O **domínio** valida os campos (nome obrigatório, data futura, capacidade
   maior que zero). Se algo estiver errado, devolve **422** com todos os
   campos inválidos de uma vez.
4. O **repository** grava no Postgres com SQL parametrizado.
5. O handler responde **201** com o evento criado e o header `Location`.

Consulta de CEP (`GET /api/v1/addresses/{cep}`): o handler chama o client do
ViaCEP, que limpa a máscara, abre a requisição com timeout, confere o status e
converte a resposta. ViaCEP fora do ar dá 502; timeout dá 504.

Ciclo de vida de um evento: nasce `draft`, vira `published` com `/publish`
(só se a data for futura) ou `cancelled` com `/cancel`. Publicar um evento
cancelado dá **409**.

Ao subir (`docker compose up`): carrega a config das variáveis de ambiente,
conecta ao banco com tentativas, roda as migrations e começa a ouvir na porta
8080. Ao receber SIGTERM (`docker stop`), para de aceitar conexões novas e
termina as que estão em andamento (graceful shutdown).

Frase de fechamento da explicação:

> "Cada camada tem uma responsabilidade só e conversa com a outra por
> interface. Por isso conseguimos testar sem banco e sem internet, e na fase 2
> podemos adicionar os outros serviços sem mexer neste."

## 1. Arquitetura e organização

### 1.1 Estrutura de pastas

```text
services/events/
  cmd/api/main.go               -> composição: liga tudo (config, pool, repo, router)
  internal/config/              -> variáveis de ambiente
  internal/domain/              -> entidade Event, regras e erros de negócio
  internal/storage/             -> repository (SQL com pgx) e conexão
  internal/transport/rest/      -> handlers, router, middlewares, JSON de resposta
  internal/clients/viacep/      -> client da API externa
  internal/observability/       -> logger
  migrations/                   -> SQL embutido (embed.FS)
```

Pontos para falar:

- Separação: **handlers** (`transport/rest`) → **regra de negócio**
  (`domain`) → **acesso a dados** (`storage`) → **client externo**
  (`clients/viacep`).
- `internal/` impede que outros módulos importem esses pacotes.
- `main.go` só monta as dependências (injeção manual) e cuida do graceful
  shutdown (`signal.NotifyContext` + `server.Shutdown`).

### 1.2 Configuração por variáveis de ambiente

- [config.go](services/events/internal/config/config.go): `DATABASE_URL`
  (obrigatória, falha no start se faltar), `HTTP_PORT` (8080), `VIACEP_URL`,
  `VIACEP_TIMEOUT`, `LOG_LEVEL`, `SHUTDOWN_TIMEOUT`, `DB_CONNECT_RETRY`.
- Defaults sensatos para o opcional; erro explícito para o obrigatório.
- `docker-compose.yml` injeta os valores. Testado em `config_test.go`.

### 1.3 go.mod

- Módulo: `github.com/KaroliniRPedrozo/ticketing/services/events`.
- Dependências diretas só 3: `chi` (roteador), `pgx/v5` (Postgres),
  `testify` (testes). O resto é `// indirect`.
- Se perguntarem "por que chi?": é leve, compatível com `net/http` padrão,
  suporta rotas aninhadas e parâmetros de URL.

## 2. Persistência própria

### 2.1 Banco próprio

- Container `events-db` (Postgres 16) exclusivo do serviço, volume próprio.
- Schema em `migrations/0001_create_events.sql` (+ seed em `0002`).
- Constraints no banco: `CHECK (capacity > 0)`, `CHECK (price_cents >= 0)`,
  `CHECK (status IN (...))`, índices por `(status, starts_at)` e `lower(city)`.
- Regra de ouro dos microsserviços: **nenhum outro serviço lê essa tabela**;
  na fase 2 a troca é só via gRPC.
- Migrations embutidas no binário (`embed.FS`), aplicadas no start.

### 2.2 Camada isolada

- [event_repository.go](services/events/internal/storage/event_repository.go):
  todo SQL mora aqui. Queries parametrizadas (`$1`, `$2`) → sem SQL injection.
- O handler depende da **interface** `EventStore` (definida no consumidor),
  não do Postgres.

### 2.3 "Não encontrado" ≠ erro de infra

- No repositório: `errors.Is(err, pgx.ErrNoRows)` → `domain.ErrNotFound`.
- Qualquer outro erro vira `fmt.Errorf("...: %w", err)` (infra).
- `Delete` usa `RowsAffected() == 0` → `ErrNotFound`.
- No handler (`fail`): `ErrNotFound` → **404**; resto → **500** sem vazar o
  detalhe (só vai pro log).
- Teste que prova: `TestGet` → "404 nao encontrado" e "500 infra distinto de 404".

## 3. Comunicação REST externa

### 3.1 Client ViaCEP (o item mais cobrado, saiba de cor)

Arquivo: [client.go](services/events/internal/clients/viacep/client.go).

| Exigência | Onde |
| --- | --- |
| Context com timeout | `context.WithTimeout(ctx, c.timeout)` + `defer cancel()`; herda cancelamento do request |
| Fecha o Body | `defer resp.Body.Close()` logo após `Do` ter sucesso |
| Confere StatusCode | `switch`: 400 → CEP inválido, 404 → não encontrado, ≠200 → indisponível |

Detalhes extras:

- `NormalizeCEP` tira a máscara (`88900-000` → `88900000`) e valida 8 dígitos
  **antes** de chamar a rede.
- O ViaCEP responde 200 com `{"erro": true}` para CEP inexistente; o client
  trata isso como `ErrCEPNotFound`.
- URL e timeout vêm de config → testes apontam para `httptest.Server`.
- `fmt.Errorf("%w: %w", ErrUnavailable, err)` preserva o erro sentinela e a
  causa (ex.: `context.DeadlineExceeded`).

### 3.2 Rotas RESTful e status

| Rota | Verbo | Sucesso | Erros |
| --- | --- | --- | --- |
| `/api/v1/events` | POST | **201** + header `Location` | 400 JSON ruim, 422 validação |
| `/api/v1/events` | GET | 200 (paginado, filtros `city`, `status`) | 400 query inválida |
| `/api/v1/events/{id}` | GET | 200 | 400 id não-UUID, 404 |
| `/api/v1/events/{id}` | PUT | 200 | 400, 404, 422 |
| `/api/v1/events/{id}` | DELETE | **204** sem corpo | 400, 404 |
| `/api/v1/events/{id}/publish` | POST | 200 | 409 transição inválida |
| `/api/v1/events/{id}/cancel` | POST | 200 | 404 |
| `/api/v1/addresses/{cep}` | GET | 200 | 400, 404, 502, 504 |
| `/healthz`, `/readyz` | GET | 200 | `/readyz` → 503 se banco cair |

Justificativas de critério:

- **201 + Location** na criação; **204** no delete.
- **400** = requisição malformada; **422** = JSON ok mas viola regra de negócio.
- **409** = conflito de estado (publicar evento cancelado).
- **502/504** = problema no serviço externo (não é 500 nosso).
- Publish/cancel como ações explícitas em vez de PATCH genérico de status.
- `healthz` (liveness) vs `readyz` (readiness, pinga o banco).

### 3.3 JSON bem definido

- Entrada: `EventInput` (tags `json`), `DisallowUnknownFields()`,
  `MaxBytesReader` de 1 MiB.
- Saída: `Event`, `Page{items,total,limit,offset}`.
- Erro: envelope único `{"error":{"code","message","fields","request_id"}}`.
- Datas em RFC3339; preço em **centavos** (`price_cents`, int64) para evitar
  float em dinheiro.

## 4. Tratamento de erros: negócio × infraestrutura

Esta é a tabela para decorar:

| Tipo | Exemplo | Origem | HTTP |
| --- | --- | --- | --- |
| Negócio | CEP inválido | `ErrInvalidCEP` | 400 |
| Negócio | CEP/evento inexistente | `ErrCEPNotFound`, `ErrNotFound` | 404 |
| Negócio | campos inválidos | `ValidationError` | 422 |
| Negócio | transição inválida | `ErrInvalidTransition` | 409 |
| Infra | ViaCEP fora do ar / resposta ruim | `ErrUnavailable` | 502 |
| Infra | timeout do ViaCEP | `ErrUnavailable` + `DeadlineExceeded` | 504 |
| Infra | banco/erro inesperado | qualquer outro | 500 (genérico) |

- Erros sentinela + `errors.Is` / `errors.As`.
- Erro de infra é **logado com detalhe**, mas o cliente recebe mensagem
  genérica (não vaza internals).
- Código: `AddressHandler.Get` e `EventHandler.fail`.

## 5. Qualidade do código Go

### 5.1 Erros idiomáticos

- `if err != nil` em toda parte, com wrap (`%w`) e contexto
  ("inserindo evento: ...").
- Sem `_` descartando erro. Se questionarem, o único ponto de atenção é onde
  o erro só é logado (`writeJSON` ao escrever resposta, pois o status já foi
  enviado e não há o que devolver).

### 5.2 Interfaces

- `EventStore`, `AddressLookup`, `Pinger`: pequenas, definidas **onde são
  usadas** (idiomático em Go: "accept interfaces, return structs").
- Ganho: handlers testados com fakes, sem banco nem internet.

### 5.3 Consistência de nomes

- Código, tipos e rotas em **inglês**; comentários e mensagens ao usuário em
  **português**. Mantido em todos os arquivos.

## 6. Testes

- `domain/event_test.go`: validação, `CanPublish`, `CanCancel`, `Status.Valid`
  (table-driven).
- `transport/rest/event_handler_test.go`: handlers com store fake. Cobre
  201/400/404/422/500, id inválido, query inválida, transições, timeout → 504,
  health.
- `clients/viacep/client_test.go`: `httptest.Server`; sucesso, erros de negócio
  e de infra (500, JSON inválido, timeout, contexto cancelado, serviço fora).
- `config/config_test.go`: obrigatória, defaults, overrides.
- Rodar: `go test ./...` dentro de `services/events`.

## 7. Demo ao vivo (2-3 min)

```bash
docker compose up --build
curl localhost:8080/healthz
curl localhost:8080/readyz
curl "localhost:8080/api/v1/addresses/01001000"       # 200
curl "localhost:8080/api/v1/addresses/123"            # 400 negócio
curl "localhost:8080/api/v1/addresses/88900000"       # 404 negócio (usado no smoke.sh)
curl -X POST localhost:8080/api/v1/events -d '{}'     # 422 com fields
curl localhost:8080/api/v1/events                      # lista com seed
curl localhost:8080/api/v1/events/00000000-0000-0000-0000-000000000000  # 404
```

Bônus de demo: derrubar o banco (`docker stop ticketing-events-db`) e mostrar
`/readyz` → 503; `/healthz` continua 200. Há também `scripts/smoke.sh`.

## 8. Perguntas prováveis

1. **Por que `defer resp.Body.Close()`?** Libera a conexão e permite reuso
   (keep-alive); sem isso há vazamento de conexões.
2. **Por que checar StatusCode?** `Do` só retorna erro em falha de transporte;
   um 500 do servidor não é `err`.
3. **Diferença entre 404 do banco e erro de conexão?** `pgx.ErrNoRows`
   vira `ErrNotFound` (404); o resto é infra (500).
4. **Por que 422 e não 400?** 400 = não consegui interpretar; 422 = entendi
   mas viola regra.
5. **Por que 502/504 no CEP?** O problema é do serviço externo, não do nosso.
6. **Como garantem que um serviço não usa o banco de outro?** Um container de
   banco por serviço; comunicação só por contrato gRPC.
7. **Por que interface no consumidor?** Desacopla e permite fakes nos testes.
8. **O que é graceful shutdown?** Para de aceitar conexões e espera as em voo
   terminarem (`server.Shutdown` com timeout).
9. **Por que preço em centavos?** Evita erro de ponto flutuante.
10. **O que falta?** gRPC, serviços de tickets e pagamentos (fase 2); testes
    de integração do repository com banco real.
