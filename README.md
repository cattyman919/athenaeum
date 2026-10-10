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
cp .env.example .env
docker compose up -d postgres redis ollama
```
