# Athenaeum Library — Monorepo

Athenaeum Library — Management & Scholarly Wayfinding System. All engineering
documentation lives in [`docs/README.md`](./docs/README.md).

| Area | Path | Stack | Docs |
| --- | --- | --- | --- |
| Backend API | [`api/`](./api/) | Golang + Gin, PostgreSQL 16 + pgvector, Redis 7 | [Architecture](./docs/01-architecture.md) |
| Frontend | [`web/`](./web/) | Next.js App Router + ShadCN UI + Tailwind | [Frontend](./docs/07-frontend.md) |
| Documentation | [`docs/`](./docs/) | Markdown + Mermaid | [Docs index](./docs/README.md) |
| Stitch UI exports | [`.artifacts/stitch/`](./.artifacts/stitch/) | JSON manifest, screenshots, HTML | [UI design](./docs/12-ui-design.md) |
| Progress tracker | [`TODO.md`](./TODO.md) | cross-session phases, checkboxes, session log | — |

Quickstart (details: [docs/09-development.md](./docs/09-development.md)):

```bash
cp .env.example .env          # set OPENROUTER_API_KEY for real AI chat
make up                       # postgres, redis, ollama, whisper
docker compose exec ollama ollama pull nomic-embed-text
make db-migrate && make seed && make reindex
make api                      # terminal 1 (http://localhost:8080)
make web                      # terminal 2 (http://localhost:3000)
```

Sign in as `demo@athenaeum.dev` (patron) or `admin@athenaeum.dev` (admin) with `athenaeum-demo`.
Quality gates: `make lint`, `make test`, `make e2e` (see docs/09).
