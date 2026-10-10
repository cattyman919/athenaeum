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
9. [Git Workflow](#9-git-workflow)

## 1. Prerequisites

| Tool | Version | Used for |
| --- | --- | --- |
| Go | 1.27 | `api/` |
| Node + pnpm | 26 / pnpm 9 | `web/` |
| Docker + Compose | v2 | postgres/redis/ollama/whisper (+ optional prod-like run) |
| `migrate` (golang-migrate CLI) | v4.18 | SQL migrations |
| `golangci-lint` | v2 (`brew install golangci-lint`) | Go lint (`make lint`, pre-commit, CI) |
| lefthook | v1 | git hooks — commit-msg (commitlint) + pre-commit lint ([§9](#9-git-workflow)) |
| Playwright | latest | E2E browser specs ([§8](#8-testing-strategy)) |
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
  whisper:
    image: onerahmet/openai-whisper-asr-webservice:v1.10.0   # faster-whisper HTTP server (POST /asr, docs/15 §5)
    environment: { ASR_ENGINE: faster_whisper, ASR_MODEL: "${WHISPER_MODEL:-small}", ASR_MODEL_PATH: /data/whisper }
    ports: ["9000:9000"]
    volumes: [whisperdata:/data]
    healthcheck: { test: ["CMD-SHELL", "curl -sf localhost:9000/openapi.json"], interval: 10s, start_period: 120s }
  api:
    build: { context: ./api, dockerfile: Dockerfile }
    environment: { DATABASE_URL: postgres://athenaeum:athenaeum@postgres:5432/athenaeum?sslmode=disable, REDIS_ADDR: redis:6379, OLLAMA_BASE_URL: http://ollama:11434, WHISPER_BASE_URL: http://whisper:9000 }
    ports: ["8080:8080"]
    depends_on: { postgres: { condition: service_healthy }, redis: { condition: service_healthy } }
  web:
    build: { context: ./web, dockerfile: Dockerfile }
    environment: { API_BASE_URL: http://api:8080, WEB_ORIGIN: "http://localhost:3000" }
    ports: ["3000:3000"]
    depends_on: [api]
volumes: { pgdata: {}, redisdata: {}, ollamadata: {}, whisperdata: {} }
```

`api` and `web` carry `profiles: [app]`, so `make up` starts only the infra services and
`make compose` (`docker compose --profile app up -d --build`) adds the app containers.

One-command startup (compose dev profile built the same way; pull the models once):

```bash
docker compose up -d --build
docker compose exec ollama ollama pull nomic-embed-text
docker compose logs -f whisper        # faster-whisper downloads WHISPER_MODEL on first start
docker compose logs -f api
```

## 3. Dockerfiles

**`api/Dockerfile`** — multi-stage Go build:

```dockerfile
FROM golang:1.27-alpine AS build
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

**`web/Dockerfile`** — pnpm build + `next start` (Node 25+ ships no corepack, so pnpm is
installed from npm; `API_BASE_URL` is a build arg because the `/api/*` rewrite is baked at build):

```dockerfile
FROM node:26-alpine AS build
WORKDIR /src
RUN npm install -g pnpm@9.15.9
ARG API_BASE_URL=http://api:8080
ENV API_BASE_URL=$API_BASE_URL
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM node:26-alpine
WORKDIR /src
ENV NODE_ENV=production
COPY --from=build /src/package.json ./package.json
COPY --from=build /src/.next ./.next
COPY --from=build /src/public ./public
COPY --from=build /src/node_modules ./node_modules
EXPOSE 3000
ENTRYPOINT ["node_modules/.bin/next", "start"]
```

## 4. Environment

Copy `.env.example` → `.env` (template at repo root). Variable semantics and
defaults: [01 §7](01-architecture.md#7-environment-variables). Never commit `.env`
(gitignore) and never log secrets ([§7](#7-logging)).

## 5. Common Commands

| Command | Effect |
| --- | --- |
| `make up` | `docker compose up -d --build` (postgres, redis, ollama, whisper) |
| `make db-migrate` | `migrate -path api/migrations -database "$DATABASE_URL" up` |
| `make seed` | `cd api && go run ./cmd/seed --mocks=./mocks` |
| `make reindex` | `cd api && go run ./cmd/reindex` |
| `make api` | `cd api && go run ./cmd/api` (`:8080`) |
| `make web` | `cd web && pnpm dev` (`:3000`) |
| `make lint` | `golangci-lint run` + `pnpm -C web lint` |
| `make test` | backend unit + service tests |
| `make e2e` | `cd web && pnpm exec playwright test` (compose stack + stubs — [§8](#8-testing-strategy)) |
| `make ci` | local replica of CI: lint + vet + unit tests + e2e |
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
slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelDebug, TimeFormat: time.Kitchen}))
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
| web unit | `pnpm test` (Vitest) | locale resolution, catalog key parity, enum label maps ([14 §12](14-localization.md#12-testing)) |
| web e2e | Playwright (Chromium) | route smoke + key journeys against the compose stack with **stub providers** ([§8.1](#81-e2e-harness)) |

### 8.1 E2E harness

`make e2e` runs `scripts/e2e-prepare.sh` (compose `postgres` + `redis` + **real `ollama`**
with `nomic-embed-text`, an isolated `athenaeum_e2e` database that is dropped, migrated,
seeded with `--offline` and reindexed with real embeddings, and a cleared Redis
rate-limit state), then `pnpm exec playwright test`. `web/playwright.config.ts` starts the
servers itself (`webServer`): the Go API on `:18080`, the production Next build on
`:13000` (rewrite target `API_BASE_URL=http://localhost:18080`), and — from the chat
milestone on — the stub provider binary `api/cmd/e2e-stubs` (OpenAI-compatible fake
ChatModel scripted as `meta`/`token`/`tool_call`/`tool_result`/`citations`/`done`,
[06 §6](06-ai-rag.md#6-sse-event-contract), plus a fixed-text Whisper `/asr` stub mirroring the real service's wire contract,
[15 §5](15-voice-input.md#5-whisper-service)) wired via
`OPENROUTER_BASE_URL` / `WHISPER_BASE_URL`; `OFFLINE_MODE=true`. Semantic search specs assert
real relevance against the real embedding model; chat and Whisper stay stubbed so the suite
needs no secrets. Auth is captured once per role (`admin` + demo patron,
[13 §6](13-mock-data-reference.md#6-demo-users--personal-library)) into Playwright
`storageState` (`web/e2e/.auth/`, gitignored); specs live in `web/e2e/`. The login bucket is
5 attempts / 15 min per IP ([03 §2](03-api-spec.md#2-rate-limiting)), so the suite budgets its
UI logins (setup uses 2) and the prepare script clears `rl:*` keys — re-run it (or
`redis-cli FLUSHALL`) before repeating the suite locally. **Chromium only in v1**
(cross-browser is deferred — [10 §5](10-roadmap.md#5-future-enhancements)). The voice
spec uses `--use-fake-device-for-media-stream --use-fake-ui-for-media-stream`.

CI runs on every push/PR via GitHub Actions (`.github/workflows/ci.yml`): `gofmt` + vet +
golangci-lint + `go test`, web lint + unit + build, and the Playwright E2E job (compose
override `docker-compose.ci.yml` keeps the Ollama model in `.ollama-cache`, persisted with
`actions/cache`). Failures upload Playwright traces. The E2E job is secret-free.

## 9. Git Workflow

Commits are **part of the workflow** — commit each completed feature or bug as it lands;
do **not** wait for an explicit request. Rules:

- **Conventional Commits**: `<type>(<scope>): <imperative summary>`.
  - **Types**: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `chore`, `build`, `ci`.
  - **Scopes**: `api`, `web`, `db`, `infra`, `docs`, `chat`, `voice`, `i18n`.
  - Body explains *why*; footer cites the TODO item or issue.
- **Granularity**: one commit per feature or bug. A task = a short-lived branch off
  `main`; commit atomically; **squash-merge** so `main` holds exactly one commit per
  feature/bug. No WIP commits on `main`.
- **Green before commit**: `make lint` + `make test` (plus `make e2e` for user-facing
  flows) must pass. Never commit secrets or `.env` ([§4](#4-environment)).
- **Hooks** (`lefthook.yml`, installed with `lefthook install`):
  - `pre-commit` — `gofmt` + `golangci-lint` on staged `api/**`; ESLint + Prettier on
    staged `web/**`.
  - `commit-msg` — `commitlint` validates the subject (`commitlint.config.js`).

```bash
git switch -c feat/voice-input          # one branch per TODO task
# ... implement + docs + tests ...
make lint && make test
git commit -m "feat(voice): add push-to-talk transcription to chat"   # hook-validated
git switch main && git merge --squash feat/voice-input && git commit  # one commit on main
```

Example subjects: `feat(chat): add locale-aware replies`, `fix(i18n): keep catalog keys
in sync`, `docs(voice): document the transcribe endpoint`, `ci: run Playwright e2e`.
