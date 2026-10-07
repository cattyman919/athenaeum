# Architecture

> Athenaeum system architecture: runtime topology, backend layering (handler → service → repository), request lifecycle, external integrations, Docker Compose topology, and environment variables.

## Contents

1. [System Overview](#1-system-overview)
2. [Backend Layers](#2-backend-layers)
3. [Request Lifecycle](#3-request-lifecycle)
4. [Streaming (SSE)](#4-streaming-sse)
5. [External Integrations](#5-external-integrations)
6. [Docker Compose Topology](#6-docker-compose-topology)
7. [Environment Variables](#7-environment-variables)
8. [Cross-cutting Rules](#8-cross-cutting-rules)

## 1. System Overview

Two deployable units (Go API, Next.js web) plus three stateful services. All DB
schemas: [02-data-model.md](02-data-model.md) · all endpoints: [03-api-spec.md](03-api-spec.md).

```mermaid
flowchart TB
    B["Browser<br/>Next.js 15 App Router (web/)"]

    subgraph W["web (Next.js)"]
        RSC["Server Components<br/>fetch() to api/ with cookies"]
        CC["Client Components<br/>admin, scanner, chat SSE"]
    end

    subgraph A["api (Go + Gin)"]
        MW["middleware/<br/>CORS, logging, auth, rate limit"]
        H["handler/<br/>HTTP only"]
        S["service/<br/>business logic"]
        AIS["service/ai<br/>Eino agent + RAG"]
        REP["repository/<br/>interfaces + pgx impl"]
    end

    PG[("PostgreSQL 16 + pgvector<br/>14 tables + 1 view")]
    RD[("Redis 7<br/>rate limits + ISBN cache")]
    OL["Ollama 11434<br/>nomic-embed-text, 768-dim"]
    OR["OpenRouter<br/>anthropic/claude-3.5-sonnet"]
    OL2["Open Library<br/>isbn metadata + covers"]

    B --> MW
    RSC --> MW
    MW --> H --> S
    S --> REP --> PG
    MW -- "login/search/chat budgets" --> RD
    S -- "Redis: ISBN lookups" --> RD
    S --> AIS
    AIS --> OL
    AIS --> OR
    S -- "POST /api/books/fetch-isbn" --> OL2
```

| Concern | Owning doc |
| --- | --- |
| SQL schema, indexes, `book_locations` view | [02-data-model.md](02-data-model.md) |
| Endpoint contracts, SSE events, rate limits | [03-api-spec.md](03-api-spec.md) |
| Auth flows, JWT claims, cookie names | [04-auth.md](04-auth.md) |
| Building hierarchy & wayfinding resolution | [05-building-layout.md](05-building-layout.md) |
| RAG pipeline, Eino agent, tools | [06-ai-rag.md](06-ai-rag.md) |
| Pages, ShadCN usage, state conventions | [07-frontend.md](07-frontend.md) |

## 2. Backend Layers

```mermaid
flowchart TB
    M["middleware<br/>CORS, slog, auth, rate limit"] --> H
    H["handler<br/>parse/validate HTTP, map errors"] --> S
    S["service<br/>business rules, availability, waitlist, RAG"] --> REP
    REP["repository<br/>SQL via pgx, table-scoped"] --> PG[("PostgreSQL")]

```

| Package | May import | Must NOT import | Example type |
| --- | --- | --- | --- |
| `internal/handler` | gin, model, service | repository, pgx | `handler.BookHandler` |
| `internal/service` | repository interfaces, model, pkg | gin | `service.CatalogService` |
| `internal/service/ai` | Eino, Ollama/OpenRouter ACL, service interfaces | gin, pgx | `service/ai.ChatService` |
| `internal/repository` | pgx, model | service | `repository.BookRepository` |
| `internal/middleware` | config, service (auth), pkg/jwt | handler | `middleware.RequireAuth` |
| `internal/model` | — (std only) | all of the above | `model.Book`, `model.DTOs` |
| `internal/pkg` | std libs only | others | `pkg/jwt`, `pkg/hash`, `pkg/isbn`, `pkg/checkdigit` |

Dependency rule (Clean Architecture): dependencies point inward — handler → service →
(repository interface). `"Repository"` interfaces declared in `internal/service`
(consumer side), implemented in `internal/repository`. Wiring happens once in
`cmd/api/main.go` (manual DI, no framework).

## 3. Request Lifecycle

Example: `GET /api/books/search?q=dune&mode=hybrid` (full contract: [03-api-spec.md](./03-api-spec.md)).

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser (web/)
    participant G as Gin Router
    participant MW as Middleware chain
    participant H as BookHandler
    participant S as CatalogService
    participant R as Repository (pgx)
    participant RD as Redis
    participant PG as Postgres + pgvector

    B->>G: GET /api/books/search?q=dune and mode=hybrid
    G->>MW: CORS, slog, RequireAuth, RateLimit
    MW->>RD: INCR rl:search:{user_id} (30/1 min)
    RD-->>MW: allowed
    MW->>H: c with user_id (claims)
    H->>H: bind + validate query (mode, page, per_page)
    H->>S: Search(ctx, SearchQuery)
    S->>R: KeywordFTS + SemanticVector queries
    R->>PG: tsvector FTS + pgvector cosine + RRF fuse
    PG-->>R: scored rows (books + availability counts)
    R-->>S: []SearchBookRow
    S-->>H: []model.SearchBookDTO with page meta
    H-->>B: 200 JSON envelope + X-RateLimit headers
```

Rules:

- One request = at most one transaction; repositories never call each other.
- Service returns typed domain errors (`model.ErrValidation`, `model.ErrNotFound`,
  `model.ErrBookUnavailable`, `model.ErrRateLimited`) → a single HTTP error mapper in
  `handler` renders the envelope ([03-api-spec.md](03-api-spec.md#error-envelope)).
- Every mutating service call flow-through is wrapped in `slog` structured logs with
  `request_id` (UUID) — see [09-development.md](09-development.md#7-logging).
- Rate limiting: Redis sliding/fixed window per token bucket table in
  [03-api-spec.md](03-api-spec.md#2-rate-limiting) (keys `rl:{bucket}:{peer}`).

## 4. Streaming (SSE)

`POST /api/chat` is the only streaming endpoint — `Content-Type: text/event-stream`.
Event names, ordering, and termination contract are owned by
[06-ai-rag.md](06-ai-rag.md#6-sse-event-contract); the browser client is specified in
[07-frontend.md](07-frontend.md#6-chat-streaming-client). Non-streaming endpoints are
plain JSON.

## 5. External Integrations

| Integration | Direction | Purpose | Env | Failure policy |
| --- | --- | --- | --- | --- |
| OpenRouter | api → openrouter.ai | chat completions (stream) via custom Eino `ChatModel` ACL adapter | `OPENROUTER_API_KEY`, `OPENROUTER_BASE_URL`, `OPENROUTER_MODEL` | 5s connect / 120s stream; outage → 502 `UPSTREAM_ERROR` |
| Ollama | api → localhost:11434 | embeddings, batch mode for reindex | `OLLAMA_BASE_URL`, `OLLAMA_EMBED_MODEL`, `OLLAMA_EMBED_DIM` | 10s per call; outage → 503 `EMBEDDING_UNAVAILABLE` |
| Open Library | api → openlibrary.org | metadata enrichment for ISBN intake | `OPENLIBRARY_BASE_URL`, `OFFLINE_MODE` | 3s timeout; fallback to mock/seeded values ([08-seed-data.md](08-seed-data.md#6-open-library-fallback)); 502 when all fail |
| Google OAuth | browser → accounts.google.com | SSO redirect + code exchange | `GOOGLE_OAUTH_CLIENT_ID/SECRET` | invalid code/state → 401 ([04-auth.md](04-auth.md#3-google-sso)) |

## 6. Docker Compose Topology

File: `docker-compose.yml` at repo root; service definitions in
[09-development.md](09-development.md#2-docker-compose). `web` proxies `/api/*` to
`api` in both dev and compose (no CORS in prod path).

```mermaid
flowchart LR
    P["Ports:<br/>3000 web · 8080 api · 5432 pg · 6379 redis · 11434 ollama"]

    subgraph DC["docker compose"]
        W["web:3000<br/>node:20-alpine, next start"] -- "rewrites /api → api:8080" --> A["api:8080<br/>golang:1.22 multi-stage"]
        A --> PG["postgres:16<br/>pgvector/pgvector:pg16"]
        A --> RD["redis:7<br/>redis:7-alpine"]
        A --> OL["ollama:11434<br/>ollama/ollama"]
        W -. "dev only" .-> NODE["host: pnpm dev"]
    end
```

| Service | Image | Port | Health check | Volumes |
| --- | --- | --- | --- | --- |
| `web` | node:20-alpine, `next start` | 3000 | GET /api/health via api proxy | — |
| `api` | golang:1.22-alpine (built binary) | 8080 | `GET /api/health` | — |
| `postgres` | `pgvector/pgvector:pg16` | 5432 | `pg_isready` | `pgdata` |
| `redis` | `redis:7-alpine` | 6379 | `redis-cli ping` | `redisdata` |
| `ollama` | `ollama/ollama` | 11434 | `ollama list` exit 0 | `ollamadata` |

## 7. Environment Variables

Loaded by `internal/config` via `caarlos0/env` into a typed `Config` struct (no os.Getenv scattered). Canonical list also mirrored in `.env.example`.

| Var | Default (dev) | Notes |
| --- | --- | --- |
| `APP_ENV` | `dev` | `dev` (tint colorized logs) / `prod` (JSON logs) |
| `API_PORT` | `8080` | |
| `WEB_ORIGIN` | `http://localhost:3000` | CORS allow + cookie domain source |
| `DATABASE_URL` | `postgres://athenaeum:athenaeum@localhost:5432/athenaeum?sslmode=disable` | pgx |
| `REDIS_ADDR` | `localhost:6379` | go-redis v9 |
| `REDIS_PASSWORD` | empty | |
| `JWT_ACCESS_SECRET` / `JWT_REFRESH_SECRET` | required in prod | HS256, min 32 bytes |
| `ACCESS_TTL` | `15m` | access token ([04-auth.md](04-auth.md#5-token-model)) |
| `REFRESH_TTL` | `168h` | 7-day refresh |
| `GOOGLE_OAUTH_CLIENT_ID/SECRET` | — | SSO ([04-auth.md](04-auth.md#3-google-sso)) |
| `OPENROUTER_API_KEY` | — | required for chat; absent → `/api/chat` returns 503 |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | |
| `OPENROUTER_MODEL` | `anthropic/claude-3.5-sonnet` | |
| `OLLAMA_BASE_URL` | `http://localhost:11434` | |
| `OLLAMA_EMBED_MODEL` | `nomic-embed-text` | |
| `OLLAMA_EMBED_DIM` | `768` | matches `book_embeddings.embedding` ([02](02-data-model.md#book_embeddings)) |
| `OPENLIBRARY_BASE_URL` | `https://openlibrary.org` | overridable for tests/proxies |
| `OFFLINE_MODE` | `false` | skips outbound metadata fetches entirely |
| `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` | set in `.env.example` | created by `cmd/seed` |
| `SEED_USER_EMAIL` / `SEED_USER_PASSWORD` | set in `.env.example` | demo patron |

## 8. Cross-cutting Rules

- **IDs:** UUID v4 generated in Go (`github.com/google/uuid`), stored as `UUID`.
- **Emails:** lowercase-normalized (`lower(email)`, unique index — [02](02-data-model.md#users)).
- **Time zone:** datetimes stored as `TIMESTAMPTZ`; `web` renders in the library building's `America/New_York`.
- **Graceful shutdown:** `SIGTERM` → 10s drain, `slog` per-route summary.
- **No loan/fining logic** anywhere (out of scope — [10-roadmap.md](./10-roadmap.md)); `checked_out` copies carry no due dates in v1.
- **Health:** `GET /api/health` returns `{"status":"ok"}` (public) — defined in [03-api-spec.md](./03-api-spec.md#41-get-apihealth).
