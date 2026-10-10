# Ticketing — Sistema de Eventos e Ingressos

Projeto de microsserviços em Go. **Apresentação I** entrega o primeiro
microsserviço (`events-service`) com comunicação externa via REST, persistência
própria em PostgreSQL, containerização e logs estruturados.

## Arquitetura

### Hoje (Apresentação I)

```
   cliente externo
        │ REST/JSON
        ▼
 ┌───────────────┐      ┌──────────────┐
 │ events-service│─────▶│  events-db   │
 │   :8080       │      │ PostgreSQL   │
 └───────────────┘      └──────────────┘
```

### Planejado (Apresentação II)

```
   cliente externo
        │ REST/JSON
        ▼
 ┌───────────────┐  gRPC  ┌────────────────┐  gRPC    ┌──────────────────┐
 │ events-service│◀──────▶│ tickets-service│◀──────▶│ payments-service │
 └───────┬───────┘        └────────┬───────┘          └─────────┬────────┘
         ▼                         ▼                          ▼
   ┌───────────┐            ┌────────────┐            ┌──────────────┐
   │ events-db │            │ tickets-db │            │ payments-db  │
   └───────────┘            └────────────┘            └──────────────┘
```

Cada serviço tem **banco próprio**: nenhum serviço lê a tabela do outro, toda
troca passa pelo contrato gRPC definido em `proto/`.

## Stack

| Camada          | Escolha                              |
|-----------------|--------------------------------------|
| Linguagem       | Go 1.23                              |
| Roteamento HTTP | `net/http` + `chi/v5`                |
| Banco           | PostgreSQL 16 + `pgx/v5` (pool)      |
| Migrations      | SQL embutido no binário (`embed.FS`) |
| Logs            | `log/slog` em JSON                   |
| Containers      | Docker + Docker Compose              |

## Como rodar

Pré-requisitos: Docker e Docker Compose. (`jq` e `make` ajudam, mas são opcionais.)

```bash
docker compose up --build
```

A API sobe em `http://localhost:8080`. O Postgres fica exposto em `localhost:5433`
caso você queira inspecionar com `psql` ou DBeaver.

Com `make`:

```bash
make up       # sobe tudo
make logs     # acompanha os logs do serviço
make smoke    # roda o roteiro de demonstração
make clean    # derruba tudo e apaga o volume do banco
```

### Rodando fora do Docker

```bash
docker compose up -d events-db
cd services/events && go mod tidy
make run
```

> Na primeira vez rode `go mod tidy` dentro de `services/events/` para gerar o
> `go.sum`. Sem ele o `docker build` falha no `go mod download`.

## Endpoints

Base: `/api/v1`

| Método | Rota                   | Descrição                                    |
|--------|------------------------|----------------------------------------------|
| GET    | `/healthz`             | Liveness — processo de pé                     |
| GET    | `/readyz`              | Readiness — inclui ping no banco              |
| POST   | `/api/v1/events`       | Cria evento (nasce como `draft`)              |
| GET    | `/api/v1/events`       | Lista com filtros `city`, `status` e paginação |
| GET    | `/api/v1/events/{id}`  | Busca por id                                  |
| PUT    | `/api/v1/events/{id}`  | Atualiza dados do evento                      |
| POST   | `/api/v1/events/{id}/publish` | `draft` → `published`                  |
| POST   | `/api/v1/events/{id}/cancel`  | → `cancelled`                          |
| DELETE | `/api/v1/events/{id}`  | Remove o evento                               |

### Exemplo

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H 'Content-Type: application/json' \
  -d '{
        "name": "Festival de Inverno",
        "description": "Dois dias de musica.",
        "venue": "Parque Central",
        "city": "Ararangua",
        "starts_at": "2027-07-10T20:00:00Z",
        "capacity": 3000,
        "price_cents": 9900
      }'
```

```json
{
  "id": "6b1f3c2a-...",
  "name": "Festival de Inverno",
  "status": "draft",
  "capacity": 3000,
  "price_cents": 9900,
  "created_at": "2026-10-05T17:00:00Z"
}
```

### Formato de erro

Todas as falhas usam o mesmo envelope, com o `request_id` para cruzar com os logs:

```json
{
  "error": {
    "code": "validation_error",
    "message": "payload invalido",
    "fields": {
      "name": "obrigatorio",
      "capacity": "deve ser maior que zero"
    },
    "request_id": "9f2c1b4e7a3d8051"
  }
}
```

| Status | Quando                                        |
|--------|-----------------------------------------------|
| 400    | id mal formado, query string inválida, JSON quebrado |
| 404    | evento inexistente ou rota desconhecida       |
| 409    | transição de status não permitida             |
| 422    | payload bem formado mas inválido              |
| 500    | erro inesperado (detalhe só no log)           |

## Observabilidade

Uma linha JSON por requisição em stdout:

```json
{"timestamp":"2026-10-05T17:04:11Z","level":"INFO","msg":"requisicao concluida",
 "service":"events-service","env":"development","request_id":"9f2c1b4e7a3d8051",
 "method":"POST","path":"/api/v1/events","status":201,"bytes":312,"duration_ms":14.3}
```

Pontos relevantes:

- **`request_id`** vem do header `X-Request-ID` ou é gerado, e volta na resposta.
  É o mesmo valor que será propagado via metadata gRPC na fase 2 — ou seja, já
  existe rastreio distribuído por correlação antes mesmo do tracing.
- Nível do log acompanha o status: `5xx` → `ERROR`, `4xx` → `WARN`, resto → `INFO`.
- Eventos de negócio (criação, mudança de status, remoção) também são logados
  com `event_id`.

Para filtrar durante a apresentação:

```bash
docker compose logs -f events-service | jq 'select(.status >= 400)'
```

## Atendimento aos requisitos

| # | Requisito                        | Situação                                                     |
|---|----------------------------------|--------------------------------------------------------------|
| 1 | 2–3 microsserviços em Go         | 1 de 3 entregue; contratos dos demais já em `proto/`          |
| 3 | Comunicação entre si e externa   | Externa pronta; interna planejada na fase 2                   |
| 4 | Comunicação externa via REST     | Pronto — `chi`, JSON, verbos e status codes corretos          |
| 5 | Comunicação interna via gRPC     | `proto/events/v1/events.proto` definido; implementação na fase 2 |
| 6 | Persistência própria             | Pronto — Postgres dedicado, migrations versionadas            |
| 7 | Docker Compose                   | Pronto — build multi-stage, healthchecks, volume nomeado      |
| 8 | Observabilidade                  | Pronto — slog JSON, request id, healthz/readyz                |

## Próximos passos (Apresentação II)

1. Gerar os stubs: `buf generate` ou `protoc --go_out --go-grpc_out`.
2. Subir o servidor gRPC do `events-service` numa segunda goroutine (porta 9090,
   não publicada no host).
3. Criar o `tickets-service`: REST para o cliente comprar, gRPC para consultar o
   evento antes de reservar.
4. Propagar o `request_id` via `metadata` em interceptor gRPC.
5. Opcional: OpenTelemetry + Jaeger, e RabbitMQ para o evento `ticket.purchased`.

## Estrutura

```
ticketing/
├── docker-compose.yml
├── Makefile
├── proto/events/v1/events.proto      # contrato gRPC (fase 2)
├── scripts/smoke.sh
└── services/events/
    ├── Dockerfile
    ├── go.mod
    ├── cmd/api/main.go               # bootstrap + graceful shutdown
    ├── internal/
    │   ├── config/                   # configuração por env
    │   ├── domain/                   # entidade, regras e validação
    │   ├── observability/            # logger JSON + contexto
    │   ├── storage/                  # pool pgx + repositório
    │   └── transport/rest/           # router, handlers, middlewares
    └── migrations/                   # SQL embutido no binário
```
