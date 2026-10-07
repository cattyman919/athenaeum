# api/ — Go backend

Gin HTTP API, service–repository architecture, PostgreSQL 16 + pgvector,
Redis 7, Eino (AI/RAG), Ollama embeddings, OpenRouter chat model.

- Architecture & layers: [`docs/01-architecture.md`](../docs/01-architecture.md)
- Database schema: [`docs/02-data-model.md`](../docs/02-data-model.md)
- HTTP API: [`docs/03-api-spec.md`](../docs/03-api-spec.md)
- Development workflow: [`docs/09-development.md`](../docs/09-development.md)

Planned layout: `cmd/{api,seed,reindex}`, `internal/{config,handler,service,repository,middleware,model,pkg}`,
`migrations/`, `mocks/`.
