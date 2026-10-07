# AGENTS.md — guidance for AI coding agents

Read these before writing any code. The docs are the contract — resolve ambiguity
by their definitions, in this order:

| # | Doc | Owns |
| --- | --- | --- |
| 1 | [docs/README.md](docs/README.md) | index, monorepo layout, traceability rules |
| 2 | [docs/01-architecture.md](docs/01-architecture.md) | layers, lifecycle, env vars |
| 3 | [docs/02-data-model.md](docs/02-data-model.md) | every table/index (canonical DDL) |
| 4 | [docs/03-api-spec.md](docs/03-api-spec.md) | every endpoint + error envelope |
| 5 | [docs/04-auth.md](docs/04-auth.md) | tokens, cookies, SSO |
| 6 | [docs/05-building-layout.md](docs/05-building-layout.md) | hierarchy, path resolvers |
| 7 | [docs/06-ai-rag.md](docs/06-ai-rag.md) | Eino agent, RAG, SSE events |
| 8 | [docs/07-frontend.md](docs/07-frontend.md) | routes, data flow, states |
| 9 | [docs/08-seed-data.md](docs/08-seed-data.md) | seed/reindex engine |
| 10 | [docs/09-development.md](docs/09-development.md) | run, build, test |
| 11 | [docs/10-roadmap.md](docs/10-roadmap.md) | scope boundaries |
| 12 | [docs/11-barcode-scanning.md](docs/11-barcode-scanning.md) | scanner feature |
| 13 | [docs/12-ui-design.md](docs/12-ui-design.md) | design system + Stitch gallery |
| 14 | [docs/13-mock-data-reference.md](docs/13-mock-data-reference.md) | fixture values |

Ground rules:

- **Monorepo**: Go code in `api/`, Next.js app in `web/`. Docs paths ending in `api/…` are relative to `api/`.
- Never invent endpoints, tables, screen IDs, or mock values; every one is defined in a doc. When docs and Stitch visuals conflict (!), the Stitch screen IDs in `12-ui-design.md` win visually; API/data shapes always come from docs 02/03.
- Anything not implemented (WeChat SSO, password reset, RFID, WebSockets, …) stays unimplemented — `> ⚠️ TODO:` markers in the docs list every deferral; do not silently add scope.
- Mock data is code-adjacent contract: exact book/shelf values in `docs/13-mock-data-reference.md`, schemas in `docs/08-seed-data.md`.
- Backend conventions (Go): handler → service → repository, typed domain errors, one error mapper. Frontend: server components for reads, URL-as-state, ShadCN/Tailwind tokens from `12-ui-design.md`.
