# Comandos da demonstração

Rode tudo no **Git Bash**, na raiz do projeto. No PowerShell o `curl` é outro
comando e as aspas do JSON dão problema.

## 1. Preparar o ambiente

| Comando | O que faz |
| --- | --- |
| `make up` | Sobe o banco e o serviço em segundo plano (`docker compose up --build -d`). A API fica em `localhost:8080`. |
| `docker compose ps` | Mostra os containers e se estão saudáveis. |
| `make logs` | Acompanha os logs estruturados do serviço (Ctrl+C para sair). |
| `make down` | Para os containers e mantém os dados. |
| `make clean` | Para os containers e **apaga o volume do banco** (recomeça do zero). |
| `make rebuild` | Recompila e reinicia só o serviço, depois de mudar o código. |

## 2. Testes automatizados

| Comando | O que faz |
| --- | --- |
| `make test` | Roda `go test ./...` dentro de `services/events`. Não precisa de banco nem de internet. |
| `cd services/events && go test ./... -v` | Mesmo teste, listando cada caso. Bom para mostrar os nomes dos testes. |

## 3. Demo guiada

| Comando | O que faz |
| --- | --- |
| `make demo` | Abre o menu interativo (`scripts/demo.sh`): criar, buscar, publicar, cancelar, remover, listar, CEP e erros. |
| `make smoke` | Percorre sozinho o ciclo completo de um evento. Precisa do `jq`. |

## 4. Sequência manual com curl

Use `-i` para mostrar a linha de status (`HTTP/1.1 422 ...`).

### 4.1 Saúde do serviço

```bash
curl -i localhost:8080/healthz
curl -i localhost:8080/readyz
```

- `/healthz` (liveness): o processo está de pé. Responde 200.
- `/readyz` (readiness): o processo está de pé **e** o banco responde. 200, ou 503 se o banco cair.

### 4.2 Listar eventos

```bash
curl "localhost:8080/api/v1/events"
curl "localhost:8080/api/v1/events?status=published&limit=5"
```

- A primeira lista os eventos do seed, com `items`, `total`, `limit` e `offset`.
- A segunda filtra por status e limita a página a 5 itens.

### 4.3 Criar um evento (201)

```bash
curl -i -X POST localhost:8080/api/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"name":"Show de Encerramento","description":"Demo","venue":"Centro de Eventos","city":"Ararangua","starts_at":"2027-03-15T22:00:00Z","capacity":1200,"price_cents":8500}'
```

Devolve **201**, o evento com `id` e `status: draft`, e o header `Location`.
Copie o `id` para os próximos passos.

### 4.4 Buscar, publicar e cancelar

```bash
curl localhost:8080/api/v1/events/<ID>
curl -X POST localhost:8080/api/v1/events/<ID>/publish
curl -X POST localhost:8080/api/v1/events/<ID>/cancel
curl -i -X POST localhost:8080/api/v1/events/<ID>/publish
```

- Buscar: 200 com o evento.
- Publicar: muda `draft` para `published`.
- Cancelar: muda para `cancelled`.
- Publicar de novo depois de cancelar: **409**, transição inválida (regra de negócio).

### 4.5 Atualizar e remover

```bash
curl -i -X PUT localhost:8080/api/v1/events/<ID> \
  -H 'Content-Type: application/json' \
  -d '{"name":"Novo nome","description":"Demo","venue":"Centro de Eventos","city":"Ararangua","starts_at":"2027-03-15T22:00:00Z","capacity":1500,"price_cents":9000}'
curl -i -X DELETE localhost:8080/api/v1/events/<ID>
```

- PUT: 200 com o evento atualizado.
- DELETE: **204**, sem corpo. Repetir o DELETE dá 404.

### 4.6 API externa (ViaCEP)

```bash
curl -i localhost:8080/api/v1/addresses/01001000
curl -i localhost:8080/api/v1/addresses/123
curl -i localhost:8080/api/v1/addresses/88900000
```

- `01001000`: **200** com o endereço.
- `123`: **400**, CEP inválido (erro de negócio, nem chega a chamar a rede).
- `88900000`: **404**, CEP não encontrado (erro de negócio).

### 4.7 Erros de negócio

```bash
curl -i -X POST localhost:8080/api/v1/events \
  -H 'Content-Type: application/json' -d '{"name":"","capacity":0}'
curl -i localhost:8080/api/v1/events/00000000-0000-0000-0000-000000000000
curl -i localhost:8080/api/v1/events/abc
curl -i localhost:8080/rota-que-nao-existe
```

| Chamada | Status | Motivo |
| --- | --- | --- |
| POST com campos vazios | 422 | Validação: devolve todos os campos inválidos de uma vez. |
| UUID que não existe | 404 | `pgx.ErrNoRows` virou `ErrNotFound`. |
| ID que não é UUID | 400 | Requisição malformada. |
| Rota inexistente | 404 | Mesmo envelope JSON de erro. |

### 4.8 Erro de infraestrutura

```bash
docker stop ticketing-events-db
curl -i localhost:8080/readyz
curl -i localhost:8080/healthz
curl -i localhost:8080/api/v1/events
docker start ticketing-events-db
```

- Com o banco parado, `/readyz` responde **503**, enquanto `/healthz` continua 200.
- Listar eventos responde **500** com mensagem genérica; o detalhe técnico só aparece no log (`make logs`).
- Depois do `docker start`, aguarde alguns segundos até o serviço voltar ao normal.

## 5. Mostrar o banco

```bash
docker exec -it ticketing-events-db psql -U events -d eventsdb -c "SELECT id, name, status FROM events;"
```

Consulta a tabela direto no Postgres do serviço. Serve para provar que o banco
é próprio e que o dado gravado pela API está lá.

## 6. Ordem sugerida (~3 min)

1. `make up` e `curl -i localhost:8080/readyz`
2. Criar evento (4.3), depois publicar (4.4)
3. Listar com filtro (4.2)
4. CEP: 200, 400 e 404 (4.6)
5. Erro de validação 422 (4.7)
6. Derrubar o banco (4.8)
7. Fechar com `make test`
