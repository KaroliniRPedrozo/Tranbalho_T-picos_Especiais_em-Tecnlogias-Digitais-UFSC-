# Explicação do código — Events Service

Este documento descreve, em linguagem corrida, como o projeto é organizado e o que cada parte do código faz. A ideia é que quem lê consiga acompanhar o caminho de uma requisição, do navegador até o banco de dados, sem precisar abrir os arquivos.

## 1. Visão geral

O projeto é um **microsserviço de eventos** (shows, palestras, workshops) escrito em **Go**. Ele faz três coisas:

1. **CRUD de eventos**: criar, listar (com filtros e paginação), buscar, atualizar e excluir.
2. **Ciclo de vida do evento**: um evento nasce como *rascunho*, pode ser *publicado* e pode ser *cancelado*, seguindo regras de negócio.
3. **Consulta de CEP**: integração com uma API externa (ViaCEP) para buscar o endereço a partir do CEP.

Ao redor do serviço existem o banco **PostgreSQL**, um **front-end** simples servido pelo **nginx** e a documentação **Swagger**. Tudo sobe junto com o Docker Compose.

```
Navegador ──► nginx (:3000) ──► events-service (:8080) ──► PostgreSQL
                │                      │
                │                      └──► ViaCEP (API externa)
                └─ serve a página (HTML/CSS/JS) e repassa /api, /docs ao serviço
```

## 2. Estrutura de pastas

```
.
├── docker-compose.yml          sobe banco, serviço e front-end
├── Makefile                    atalhos (make up, make test, make demo...)
├── proto/events/v1/            contrato gRPC (fase 2)
├── scripts/                    demo.sh e smoke.sh (demonstração e teste rápido)
├── docs/                       roteiro, comandos e esta explicação
├── frontend/                   página web (nginx)
└── services/events/            o microsserviço em Go
    ├── cmd/api/main.go         ponto de entrada
    ├── migrations/             SQL + código que aplica as migrations
    └── internal/
        ├── config/             leitura das variáveis de ambiente
        ├── domain/             entidades e regras de negócio
        ├── storage/            acesso ao PostgreSQL
        ├── clients/viacep/     cliente da API externa de CEP
        ├── observability/      logs estruturados e request id
        └── transport/
            ├── rest/           handlers HTTP, rotas e middlewares
            └── apidocs/        Swagger UI e especificação OpenAPI
```

A pasta `internal/` é uma convenção do Go: o que está dentro só pode ser importado por este próprio módulo. As camadas são separadas por responsabilidade, e cada uma só conhece a camada de baixo.

## 3. O ponto de entrada: `cmd/api/main.go`

O `main()` só chama `run()` e, se houver erro, registra no log e sai com código 1. Toda a montagem está em `run()`, que devolve erro em vez de encerrar o processo, o que facilita testar.

A sequência de inicialização é:

1. **Configuração**: lê as variáveis de ambiente (`config.Load()`).
2. **Logger**: cria um logger JSON e o define como padrão do processo.
3. **Sinais**: cria um contexto que é cancelado quando o processo recebe `SIGINT` ou `SIGTERM`. O Docker manda `SIGTERM` ao parar um container.
4. **Banco**: conecta ao PostgreSQL com tentativas (`storage.Connect`).
5. **Migrations**: cria/atualiza as tabelas antes de aceitar requisições.
6. **Injeção de dependências**: cria o repositório e o cliente do ViaCEP e entrega ambos ao roteador.
7. **Servidor HTTP**: configurado com *timeouts* (`ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`) para não ficar preso a clientes lentos.
8. **Execução e encerramento**: o servidor roda em uma goroutine, e o `main` espera ou um erro do servidor ou o sinal de parada. Ao receber o sinal, faz o ***graceful shutdown***: para de aceitar conexões novas e espera as requisições em andamento terminarem (até o `ShutdownTimeout`).

A variável `version` pode ser trocada no build com `-ldflags` e aparece no `/healthz`.

## 4. Configuração: `internal/config`

Toda a configuração vem de **variáveis de ambiente**, que é o que o Docker Compose injeta. A função `Load()` preenche uma struct `Config` aplicando valores padrão (porta 8080, nível de log `info`, timeout do ViaCEP de 5s etc.). Apenas `DATABASE_URL` é obrigatória: se faltar, o serviço falha logo na partida, em vez de subir quebrado. Três funções auxiliares (`getenv`, `getint`, `getduration`) fazem a leitura e caem no padrão quando a variável falta ou é inválida.

## 5. Domínio: `internal/domain/event.go`

É o coração do negócio e **não depende de nada** (nem banco, nem HTTP).

- **`Event`**: a entidade completa (id, nome, descrição, local, cidade, data, capacidade, preço em centavos, status, datas de criação e atualização). O preço é guardado em **centavos** (`int64`) para evitar erros de arredondamento com decimais.
- **`Status`**: `draft`, `published` ou `cancelled`, com o método `Valid()`.
- **`EventInput`**: o que a API aceita em POST e PUT. Tem `Normalize()` (remove espaços nas pontas) e `Validate()`.
- **`Validate()`** aplica as regras: nome obrigatório e até 160 caracteres, descrição até 2000, local e cidade obrigatórios, data obrigatória e **futura**, capacidade maior que zero e preço não negativo. Em vez de parar no primeiro erro, junta **todos** os problemas num `ValidationError` (um mapa campo → mensagem), e o cliente recebe tudo de uma vez.
- **Regras de transição**: `CanPublish()` bloqueia publicar evento cancelado ou com data no passado (publicar um já publicado é aceito, ou seja, é idempotente). `CanCancel()` permite cancelar de qualquer estado.
- **Erros de domínio**: `ErrNotFound` e `ErrInvalidTransition`. A camada HTTP traduz cada um num status adequado.
- **`ListFilter` e `Page`**: parâmetros de filtro/paginação e o envelope de resposta das listagens (itens, total, limit, offset).

## 6. Persistência: `internal/storage`

### `postgres.go`: conexão

`Connect` lê a URL, define limites do pool (no máximo 10 conexões, mínimo 2, com tempo de vida e de ociosidade) e cria o pool com `pgx`. Como criar o pool não garante que o banco esteja de pé, ele faz **até N tentativas** de `Ping`, esperando 1s, 2s, 3s... entre elas. Isso cobre o caso do banco ainda estar iniciando ou reiniciando. Se o serviço for parado durante a espera, ele desiste imediatamente.

### `event_repository.go`: o repositório

O `EventRepository` concentra **todo o SQL**. O resto da aplicação só conhece a interface. Pontos importantes:

- Todas as consultas usam **parâmetros** (`$1`, `$2`...), nunca concatenação de valores, o que previne **SQL injection**.
- `Create`, `Update` e `UpdateStatus` usam `RETURNING`, que devolve a linha já gravada sem uma segunda consulta.
- Uma linha inexistente (`pgx.ErrNoRows`, ou `RowsAffected() == 0` no delete) vira `domain.ErrNotFound`. É erro de negócio, diferente de uma falha de infraestrutura, que é embrulhada com `fmt.Errorf("...: %w", err)` para manter a causa.
- `List` monta o `WHERE` dinamicamente conforme os filtros (cidade sem diferenciar maiúsculas, status), faz um `count(*)` para o total, depois busca só a página pedida ordenada por data (`LIMIT/OFFSET`).
- `Ping` é usado pelo endpoint de *readiness*.

## 7. Migrations: `migrations/`

Os arquivos `.sql` são **embutidos no binário** com `//go:embed`. Assim o deploy é um artefato único: não é preciso copiar SQL para a imagem nem subir um container só para migrar.

`Apply` cria a tabela `schema_migrations`, lista os `.sql` em ordem de nome (`0001`, `0002`...) e, para cada um ainda não registrado, executa e grava o registro. Rodar duas vezes não tem efeito. Usa o protocolo simples do `pgx` para permitir vários comandos no mesmo arquivo.

- `0001_create_events.sql`: cria a tabela `events` com `CHECK` no banco (capacidade > 0, preço >= 0, status válido) e dois índices (status + data, e cidade em minúsculas). O banco também protege a integridade, não só o código.
- `0002_seed_events.sql`: insere eventos de exemplo para a demonstração.

## 8. Cliente de CEP: `internal/clients/viacep`

Encapsula a chamada à API externa **ViaCEP**. O `Lookup` segue estes passos:

1. Normaliza o CEP (remove máscara: `88900-000` → `88900000`) e exige 8 dígitos. Se for inválido, **nem chama** a API externa.
2. Aplica um **timeout** à chamada inteira, herdando o cancelamento do contexto recebido.
3. Faz o GET e confere o status HTTP: 400 vira CEP inválido, 404 vira CEP não encontrado, qualquer outro diferente de 200 vira "indisponível".
4. Trata um detalhe do ViaCEP: para CEP inexistente ele responde **200** com `{"erro": true}`, então esse caso também vira "não encontrado".
5. Converte os campos do ViaCEP (`logradouro`, `bairro`, `localidade`, `uf`) para o formato próprio da API (`street`, `neighborhood`, `city`, `state`).

Os erros são divididos em dois tipos: **de negócio** (`ErrInvalidCEP`, `ErrCEPNotFound`, culpa de quem chamou) e **de infraestrutura** (`ErrUnavailable`, serviço externo fora do ar ou lento). Essa separação define o status HTTP final. A URL base vem da configuração, o que permite apontar para um servidor falso nos testes.

## 9. Camada HTTP: `internal/transport/rest`

### `router.go`: as rotas

Usa o roteador **chi**. Registra três middlewares (nesta ordem): `RequestID`, `StructuredLogger` e `Recoverer`. Depois define:

| Rota | O que faz |
|---|---|
| `GET /healthz` | *Liveness*: o processo está de pé |
| `GET /readyz` | *Readiness*: o processo está de pé **e** o banco responde (503 se não) |
| `POST /api/v1/events` | Cria evento |
| `GET /api/v1/events` | Lista com filtros (`city`, `status`, `limit`, `offset`) |
| `GET /api/v1/events/{id}` | Busca um evento |
| `PUT /api/v1/events/{id}` | Atualiza |
| `DELETE /api/v1/events/{id}` | Remove (204) |
| `POST /api/v1/events/{id}/publish` | Publica |
| `POST /api/v1/events/{id}/cancel` | Cancela |
| `GET /api/v1/addresses/{cep}` | Consulta CEP |
| `GET /docs`, `GET /openapi.yaml` | Swagger UI e especificação |

Também define respostas JSON padronizadas para rota inexistente (404) e método não permitido (405).

### `event_handler.go`: os handlers de eventos

Os handlers recebem o repositório por **interface** (`EventStore`), declarada no lado de quem consome. Isso permite trocar o Postgres por um *fake* nos testes. O fluxo típico de um handler é: validar a entrada → chamar a camada de baixo → traduzir o resultado em HTTP.

- **`Create`**: valida o corpo, grava e responde **201** com o cabeçalho `Location` apontando para o novo recurso.
- **`List`**: valida cada parâmetro de consulta (status precisa ser válido, `limit` positivo e limitado a 100, `offset` não negativo) e devolve 400 se algo estiver errado.
- **`Get` / `Update` / `Delete`**: validam que o `id` é um UUID antes de ir ao banco.
- **`Publish` / `Cancel`**: são **transições de estado explícitas**, em vez de um PATCH genérico de status. Ficam mais claras e permitem aplicar a regra de negócio (`CanPublish`/`CanCancel`). Se a regra for violada, a resposta é **409 Conflict**.
- **`decodeInput`**: limita o corpo a 1 MiB, **recusa campos desconhecidos**, normaliza e valida. Erro de JSON vira 400; erro de regra vira **422** com o mapa de campos.
- **`fail`**: traduz erros de domínio em status HTTP. Um erro inesperado vira **500** com mensagem genérica, e o detalhe técnico vai só para o log, para não vazar informação interna.

### `address_handler.go`: CEP

Chama o cliente e traduz o resultado: sucesso → 200; CEP malformado → 400; não existe → 404; serviço externo fora → **502** (ou **504** se foi timeout), e nunca 500, porque a falha não é do nosso serviço.

### `response.go`: formato de resposta

`writeJSON` escreve status e corpo em JSON. `writeError` monta o **envelope único de erro**: `{"error": {"code", "message", "fields", "request_id"}}`. Um formato só facilita a vida de quem consome a API.

### `middleware.go`: os middlewares

- **`RequestID`**: aceita o `X-Request-ID` enviado pelo cliente ou gera um novo (24 caracteres hexadecimais aleatórios) e o devolve no cabeçalho da resposta. Serve para correlacionar uma requisição com seus logs, e na fase 2 será repassado aos outros serviços.
- **`StructuredLogger`**: coloca no contexto um logger já enriquecido (request id, método, caminho) e, ao final, escreve **uma linha JSON por requisição** com status, bytes, duração em ms, IP e user-agent. O nível depende do resultado: 5xx é *error*, 4xx é *warn*, o resto é *info*. Para saber o status, usa um `statusRecorder` que embrulha o `ResponseWriter`.
- **`Recoverer`**: captura qualquer `panic`, registra e responde 500 no formato padrão, em vez de derrubar o processo.

## 10. Observabilidade: `internal/observability`

Cria o logger com o pacote `slog` da biblioteca padrão, escrevendo **JSON em stdout** (formato que qualquer coletor entende, como `docker logs`, Loki ou ELK), com os campos `service` e `env` em todas as linhas e o timestamp renomeado para `timestamp`. Também guarda o logger e o request id no `context.Context`, para que qualquer camada interna possa logar já com o id da requisição, sem receber o logger como parâmetro.

## 11. Documentação da API: `internal/transport/apidocs`

O arquivo `openapi.yaml` (escrito à mão) descreve todas as rotas, parâmetros, modelos e códigos de resposta, e é embutido no binário. O `docs.go` serve esse arquivo em `/openapi.yaml` e uma página do **Swagger UI** em `/docs`, que carrega a interface por CDN e permite testar a API pelo navegador. Como a especificação é manual, precisa ser atualizada quando uma rota ou campo mudar.

## 12. Front-end: `frontend/`

Uma página estática, sem framework, dividida por responsabilidade:

- `index.html`: apenas a estrutura.
- `css/style.css`: estilos (com tema claro/escuro automático).
- `js/api.js`: função `api()` que chama o backend e transforma erros do envelope em mensagens legíveis, além de utilitários (aviso temporário, formatação de moeda e data, escape de HTML para evitar XSS).
- `js/events.js`: lista, formulário de criação/edição e as ações de publicar, cancelar, editar e excluir.
- `js/cep.js`: consulta de CEP.
- `js/main.js`: liga os botões às funções e carrega a lista inicial.

O **nginx** serve esses arquivos e funciona como **proxy reverso**: tudo que começa com `/api`, `/docs`, `/openapi.yaml`, `/healthz` ou `/readyz` é repassado ao `events-service`. Assim o navegador conversa com um único endereço e não há problema de CORS.

## 13. Infraestrutura

- **`services/events/Dockerfile`**: *build* em duas etapas. A primeira compila em `golang:alpine` (com a camada de dependências separada, para aproveitar cache) um binário estático (`CGO_ENABLED=0`). A segunda copia só o binário para uma imagem `alpine` mínima e roda com **usuário sem privilégios**.
- **`docker-compose.yml`**: define `events-db` (Postgres com *healthcheck*), `events-service` (só inicia quando o banco está saudável, tem seu próprio *healthcheck* e reinicia sozinho) e `frontend`. Os três ficam na mesma rede interna.
- **`Makefile`** e **`scripts/`**: atalhos para subir, derrubar, ver logs, rodar testes, um teste rápido de fumaça (`smoke.sh`) e um menu interativo de demonstração (`demo.sh`).

## 14. Testes

Existem testes de unidade para as regras de domínio (`event_test.go`), a configuração (`config_test.go`), o cliente do ViaCEP (`client_test.go`, contra um servidor HTTP falso) e os handlers REST (`event_handler_test.go`, com um repositório *fake* no lugar do Postgres). Isso só é possível porque as dependências são injetadas por interface.

## 15. Fase 2: contrato gRPC (`proto/events/v1/events.proto`)

O arquivo define o contrato do `EventsService` para a comunicação **interna** entre serviços: `GetEvent` e `CheckAvailability`. A ideia é que um futuro `tickets-service` consulte o status e a capacidade do evento antes de reservar ingressos. **Atualmente é só o contrato**: o servidor gRPC ainda não está implementado, e hoje o serviço expõe apenas REST.

## 16. Decisões de projeto

- **Camadas com dependência para baixo** (HTTP → domínio/armazenamento): cada parte pode ser testada e trocada sozinha.
- **Interfaces no consumidor**: o handler declara o que precisa, e quem implementa (Postgres ou fake) se encaixa.
- **Erro de negócio ≠ erro de infraestrutura**: define se a resposta é 4xx (culpa do cliente), 5xx/502/504 (culpa do sistema ou de um serviço externo), e evita vazar detalhes internos.
- **Validação em camadas**: HTTP (formato e UUID), domínio (regras) e banco (`CHECK`).
- **Observabilidade desde o início**: logs JSON com *request id*, endpoints de *liveness* e *readiness*.
- **Resiliência**: retry na conexão com o banco, timeout em chamadas externas, `Recoverer` e *graceful shutdown*.
- **Configuração por ambiente**: o mesmo binário roda em qualquer ambiente.
