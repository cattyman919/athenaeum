# Development Guide

> Local development and container workflow: prerequisites, Docker Compose services, Dockerfiles, `.env.example`, common commands, dev loop, `slog` + `tint` logging, and testing strategy — env var semantics in [01-architecture.md](01-architecture.md#7-environment-variables).

## Contents

1. [Prerequisites](#1-prerequisites)
2. [Docker Compose](#2-docker-compose)
3. [Dockerfiles](#3-dockerfiles)
4. [Environment](#4-environment)
5. [Common Commands](#5-common-commands)
6. [Local Dev Workflow](#6-local-dev-workflow)
7. [Logging](#7-logging)
8. [Testing Strategy](#8-testing-strategy)

## 1. Prerequisites

| Tool | Version | Used for |
| --- | --- | --- |
| Go | 1.22 | `api/` |
| Node + pnpm | 20 LTS / pnpm 9 | `web/` |
| Docker + Compose | v2 | postgres/redis/ollama (+ optional prod-like run) |
| `migrate` (golang-migrate CLI) | v4.18 | SQL migrations |
| Git | any | hooks below |

## 2. Docker Compose

Repo-root `docker-compose.yml` (topology diagram: [01 §6](01-architecture.md#6-docker-compose-topology)).

```yaml
services:
  postgres:
    image: pgvector/pgvector:pg16
    environment: { POSTGRES_USER: athenaeum, POSTGRES_PASSWORD: athenaeum, POSTGRES_DB: athenaeum }
    ports: ["5432:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck: { test: ["CMD-SHELL", "pg_isready -U athenaeum"], interval: 5s }
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    volumes: [redisdata:/data]
    healthcheck: { test: ["CMD", "redis-cli", "ping"], interval: 5s }
  ollama:
    image: ollama/ollama
    ports: ["11434:11434"]
    volumes: [ollamadata:/root/.ollama]
  api:
    build: { context: ./api, dockerfile: Dockerfile }
    environment: { DATABASE_URL: postgres://athenaeum:athenaeum@postgres:5432/athenaeum?sslmode=disable, REDIS_ADDR: redis:6379, OLLAMA_BASE_URL: http://ollama:11434 }
    ports: ["8080:8080"]
    depends_on: { postgres: { condition: service_healthy }, redis: { condition: service_healthy } }
  web:
    build: { context: ./web, dockerfile: Dockerfile }
    environment: { API_BASE_URL: http://api:8080, WEB_ORIGIN: "http://localhost:3000" }
    ports: ["3000:3000"]
    depends_on: [api]
volumes: { pgdata: {}, redisdata: {}, ollamadata: {} }
```

One-command startup (compose dev profile built the same way; pull the embed model once):

```bash
docker compose up -d --build
docker compose exec ollama ollama pull nomic-embed-text
docker compose exec web node -e "console.log('proxy /api -> api:8080 ready')"
docker compose logs -f api
```

## 3. Dockerfiles

**`api/Dockerfile`** — multi-stage Go build:

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api

FROM alpine:3.19
COPY --from=build /bin/api /bin/api
EXPOSE 8080
ENTRYPOINT ["/bin/api"]
```

**`web/Dockerfile`** — pnpm build + standalone output:

```dockerfile
FROM node:20-alpine AS build
WORKDIR /src
COPY package.json pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM node:20-alpine
WORKDIR /src
COPY --from=build /src/.next ./.next
COPY --from=build /src/public ./public
COPY --from=build /src/node_modules ./node_modules
EXPOSE 3000
ENTRYPOINT ["pnpm", "start"]
```

## 4. Environment

Copy `.env.example` → `.env` (template at repo root). Variable semantics and
defaults: [01 §7](01-architecture.md#7-environment-variables). Never commit `.env`
(gitignore) and never log secrets ([§7](#7-logging)).

## 5. Common Commands

| Command | Effect |
| --- | --- |
| `make up` | `docker compose up -d --build` (postgres, redis, ollama) |
| `make db-migrate` | `migrate -path api/migrations -database "$DATABASE_URL" up` |
| `make seed` | `cd api && go run ./cmd/seed --mocks=./mocks` |
| `make reindex` | `cd api && go run ./cmd/reindex` |
| `make api` | `cd api && go run ./cmd/api` (`:8080`) |
| `make web` | `cd web && pnpm dev` (`:3000`) |
| `make lint` | `golangci-lint run` + `pnpm -C web lint` |
| `make test` | backend unit + service tests |
| `make compose` | full build/start of api+web containers (prod-like) |
| `make down` | `docker compose down` (keeps volumes; `-v` note below) |

## 6. Local Dev Workflow

1. `make up` then `make db-migrate` (first run only) → 2. `make seed` →
3. `make reindex` → 4. `make api` **and** `make web` (two tabs).
5. Log in at `http://localhost:3000` with admin or demo patron
   (seeds: [13 §demo users](13-mock-data-reference.md#6-demo-users--personal-library)).
6. After model/chunker changes: `make reindex` — see [08 §reindex](08-seed-data.md#7-reindex).

**Go tooling**: `gofmt`, `go vet`, `golangci-lint` (`goimports`-based). **Web**: ESLint +
Prettier via Next.js default settings.

## 7. Logging

`log/slog` everywhere; handlers emit per-request lines with `request_id`:

```go
// dev: colorized tint handler (console / tty)
slog.New(tint.NewHandler(os.Stderr, &tint.Options{Level: slog.LevelDebug, TimeFormat: time.Kitchen}))
// prod (APP_ENV=prod): JSON handler
slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
```

| Rule | Detail |
| --- | --- |
| Request id | generated per request (or `X-Request-Id` passthrough), echoed to response — [03 §1](03-api-spec.md#1-conventions) |
| Levels | `DEBUG` dev-only queries; `INFO` route access; `WARN` upstream failures; `ERROR` handled panics |
| Never logged | passwords, tokens/cookies, chat message bodies (lengths + ids only) |
| Observability | beyond logs = out of scope (Prometheus/Grafana/tracing deferred — [10-roadmap.md](10-roadmap.md)) |

## 8. Testing Strategy

| Layer | Tooling | Notes |
| --- | --- | --- |
| `pkg` | plain `go test` (jwt, hash, isbn, checkdigit) | table-driven, no mocks |
| repository | testcontainers-go with the `pgvector/pgvector:pg16` image (or the running `docker compose` postgres) | golden SQL fixtures from `api/mocks/` |
| service | interface mocking | hand-written lightweight fakes (no codegen dependency) |
| handler | `httptest` + service mocks | HTTP error envelope coverage per code ([03 §9](03-api-spec.md#9-error-catalog)) |
| AI chat service | fake Eino streams (scripted tokens + tool events) | SSE ordering is contractual — [06 §6](06-ai-rag.md#6-sse-event-contract) |
| web | — | unit/integration coverage deferred: [10-roadmap.md](10-roadmap.md) |

> ⚠️ TODO: CI pipeline (GitHub Actions: lint + vet + go tests are out-of-scope of
> the 3-week window; roadmap backlog).
