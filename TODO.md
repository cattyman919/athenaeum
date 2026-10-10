# TODO.md — Cross-Session Progress Tracker

> Living checklist for the Athenaeum build, designed for handoff between AI sessions and humans. Every task cites its defining doc; sessions update only this file plus the code, append one Session Log row, and never silently re-scope.

## Contents

1. [How to Use This File](#1-how-to-use-this-file)
2. [Current State](#2-current-state)
3. [Phase 0 — Repo & Scaffolding](#3-phase-0--repo--scaffolding)
4. [Phase 1 — Infrastructure & DB](#4-phase-1--infrastructure--db)
5. [Phase 2 — Auth](#5-phase-2--auth)
6. [Phase 3 — Seed Data & Embeddings](#6-phase-3--seed-data--embeddings)
7. [Phase 4 — Catalog & Search](#7-phase-4--catalog--search)
8. [Phase 5 — Book Detail & Wayfinding](#8-phase-5--book-detail--wayfinding)
9. [Phase 6 — Personal Library & Waitlist](#9-phase-6--personal-library--waitlist)
10. [Phase 7 — AI Librarian Chat](#10-phase-7--ai-librarian-chat)
11. [Phase 8 — Barcode Scanner](#11-phase-8--barcode-scanner)
12. [Phase 9 — Admin Screens](#12-phase-9--admin-screens)
13. [Phase 10 — Polish & Verification](#13-phase-10--polish--verification)
14. [Phase 11 — Localization](#14-phase-11--localization)
15. [Phase 12 — Voice Input](#15-phase-12--voice-input)
16. [Phase 13 — Git Workflow and CI](#16-phase-13--git-workflow-and-ci)
17. [Open TODO Registry (from docs)](#17-open-todo-registry-from-docs)
18. [Session Log](#18-session-log)

## 1. How to Use This File

Rules for any AI session (or human) continuing this project:

1. Read [AGENTS.md](AGENTS.md) → [docs/README.md](docs/README.md) → the doc cited by the task you pick up.
2. Work top-down within the current phase; do not skip ahead across phase boundaries without logging why.
3. While implementing: update the checkboxes in **this** file as tasks complete; add new discovered tasks here instead of inventing scope in code.
4. "Complete" means: code compiles, relevant `make lint` / `make test` pass, and behavior matches the owning doc. Do not tick otherwise.
5. Never mark deferred features ([§14](#14-open-todo-registry-from-docs)) as done — they stay ⚠️ TODO until [10-roadmap.md](docs/10-roadmap.md) changes.
6. End the session with a Session Log row: date, engine/session id, tasks completed, current cursor ("resume from: …"), blockers.
7. Mock/book/shelf values are a contract: change data only by editing `api/mocks/` + [docs/13-mock-data-reference.md](docs/13-mock-data-reference.md) together.

## 2. Current State

| Field | Value |
| --- | --- |
| Goal / phase | Phase 0 in progress (docs committed; Makefile/compose/hooks/CI landing). Execution order follows `.kilo/plans/*-athenaeum-v1-implementation-plan.md` milestones M0–M12; checkboxes below are still ticked per item |
| Git | `main` with commits; remote `origin = git@github.com:cattyman919/athenaeum.git`; one branch per task, squash to `main`, push `main` per milestone ([docs/09 §9](docs/09-development.md#9-git-workflow)) |
| Docs | all 16 files in `docs/` complete + link-audited (14 localization, 15 voice input added [docs/10 §1](docs/10-roadmap.md#1-in-scope-v1--checklist)) |
| Stitch artifacts | 5 HTML + 1 PNG exported (`.artifacts/stitch/`); re-exports pending + chat-screen regen for the mic ([§17](#17-open-todo-registry-from-docs)) |
| Stack pins | Go 1.27 · Next.js 15 · Postgres 16 + pgvector · Redis 7 · Ollama `nomic-embed-text` (768) · OpenRouter Claude 3.5 Sonnet · faster-whisper `small` · next-intl · Playwright |
| Demo accounts | `admin@` / `demo@athenaeum.dev` · password `athenaeum-demo` ([docs/13 §6](docs/13-mock-data-reference.md#6-demo-users--personal-library)) |

## 3. Phase 0 — Repo & Scaffolding

Refs: [docs/README.md §5](docs/README.md#5-monorepo-layout), [docs/09](docs/09-development.md#5-common-commands).

- [x] `git init` + monorepo layout (`api/`, `web/`, `docs/`, `.artifacts/`)
- [x] `.gitignore`, `.env.example`, root `README.md`, `AGENTS.md`, `TODO.md`
- [x] All 14 docs written + cross-link audit clean
- [x] First commit of docs + scaffolding
- [x] Root `Makefile` with targets from [docs/09 §5](docs/09-development.md#5-common-commands)
- [x] `docker-compose.yml` at root (per [docs/09 §2](docs/09-development.md#2-docker-compose))
- [x] `api/Dockerfile` + `web/Dockerfile` ([docs/09 §3](docs/09-development.md#3-dockerfiles)); both build ([docs/09 §3](docs/09-development.md#3-dockerfiles))

## 4. Phase 1 — Infrastructure & DB

Refs: [docs/01](docs/01-architecture.md), [docs/02](docs/02-data-model.md).

- [x] `docker compose up` green: postgres (pgvector/pg16) + redis + ollama healthy
- [x] `docker compose exec ollama ollama pull nomic-embed-text`
- [ ] `api/go.mod` (module created; gin, env, uuid, tint in — rest added as each lands; module + gin, pgx v5, go-redis v9, jwt v5, uuid, argon2, checkdigit, env, eino)
- [x] `internal/config` typed env loader (all vars from [docs/01 §7](docs/01-architecture.md#7-environment-variables))
- [x] Migrations `0001_extensions.sql` → all `CREATE TABLE` DDL verbatim from [docs/02 §4](docs/02-data-model.md#4-table-ddl-canonical-sql)
- [x] FTS + trigram + HNSW indexes ([docs/02 §5](docs/02-data-model.md#5-indexes))
- [x] `book_locations` view ([docs/02 §6](docs/02-data-model.md#6-book_locations-view))
- [x] `updated_at` trigger + `slog` request-id plumbing ([docs/09 §7](docs/09-development.md#7-logging))
- [x] `GET /api/health` ([docs/03 §4.1](docs/03-api-spec.md#41-get-apihealth))

## 5. Phase 2 — Auth

Refs: [docs/04](docs/04-auth.md); endpoints [docs/03 §4](docs/03-api-spec.md#4-auth-endpoints).

- [x] `pkg/hash` argon2id (m=65536,t=3,p=2, PHC format — [docs/04 §6](docs/04-auth.md#6-password-hashing))
- [x] `pkg/jwt` HS256 claims helper ([docs/04 §5](docs/04-auth.md#5-token-model))
- [x] register / login / me handlers + typed errors (`EMAIL_TAKEN`, `INVALID_CREDENTIALS`)
- [x] refresh-token rotation + replay detection (`REUSED_TOKEN`), logout
- [x] Google SSO redirect flow (`/api/auth/google` + callback, state cookie) — verified only against a fake OIDC server; manual Google check pending credentials
- [x] `requireAuth` / `requireAdmin` middleware; role guard tests
- [x] Redis rate limiter middleware + all buckets from [docs/03 §2](docs/03-api-spec.md#2-rate-limiting) + headers
- [x] `web/`: pnpm init, ShadCN + tokens in `tailwind.config.ts`/`globals.css` ([docs/12 §3](docs/12-ui-design.md#3-design-tokens))
- [x] `web/` AuthProvider + middleware guard + refresh retry ([docs/07 §4](docs/07-frontend.md#4-auth-context--guards))
- [x] `/login` page per Stitch screen 1 (role tabs, disabled WeChat — no SAML row) · [docs/12 §8 screen 1](docs/12-ui-design.md#screen-1--login-institution-gateway)

## 6. Phase 3 — Seed Data & Embeddings

Refs: [docs/08](docs/08-seed-data.md), values [docs/13](docs/13-mock-data-reference.md).

- [x] `api/mocks/building.json` + `books.json` with the exact fixture values of [docs/13 §2–4](docs/13-mock-data-reference.md#2-book-catalog-all-40)
- [x] `internal/service/building.ResolvePath` ([docs/05 §4](docs/05-building-layout.md#4-wayfinding-resolution))
- [x] `cmd/seed`: users → tree → books → copies; barcode counter (`ATH-002941-…`, [docs/08 §5](docs/08-seed-data.md#5-barcode-allocation)); deterministic, idempotent, `--reset`
- [x] Seed failure-mode tests ([docs/08 §8](docs/08-seed-data.md#8-failure-modes))
- [x] Copy ledger parity: 60 copies / 12 `checked_out` / 5 zero-copy verified after seed run
- [x] `internal/service/olbooks` Open Library client + fallbacks ([docs/08 §6](docs/08-seed-data.md#6-open-library-fallback))
- [x] `cmd/reindex` chunker v1 (~300 tok, 50 overlap) + Ollama batch + upsert ([docs/06 §2](docs/06-ai-rag.md#2-rag-pipeline))

- [x] Demo `user_library` (24 rows) + `waitlist` (2 rows) seeds per [docs/13 §6](docs/13-mock-data-reference.md#6-demo-users--personal-library) — lands with Phase 6 (needs those features)

## 7. Phase 4 — Catalog & Search

Refs: [docs/02 §7 query examples](docs/02-data-model.md#7-query-examples), [docs/03 §5.1](docs/03-api-spec.md#51-get-apibookssearch), [docs/07 page map](docs/07-frontend.md#7-page-breakdowns).

- [x] `repository`: keyword (tsvector), semantic (pgvector top-20), hybrid (RRF k=60)
- [x] `service`: facets (section/availability/era), pagination envelope, `availability.state` resolution
- [x] `GET /api/books/search` handler (30/1min bucket; `X-RateLimit-*`)
- [x] `web/middleware.ts` + `lib/api-server.ts` cookie forwarding
- [x] `/catalog` page: tri-modal switcher, facet sidebar, BookCard 2:3 grid, skeletons
- [x] ⌘K `CommandPalette` (client-only) wired to the same endpoint

## 8. Phase 5 — Book Detail & Wayfinding

Refs: [docs/03 §5.2](docs/03-api-spec.md#52-get-apibooksid), [docs/05 §3–5](docs/05-building-layout.md#3-location-path-tokens).

- [x] `GET /api/books/{id}`: copies + `book_locations` join + `neighbors` carousel data
- [x] `BookLocationCard` wayfinding ladder (`MAIN/3F/SF/A-1/S-2` display format)
- [ ] (banner done; waitlist CTA lands with M7) Tri-state availability banner + waitlist CTA on zero-available ([§9 below](#9-phase-6--personal-library--waitlist))
- [x] `/books/[id]` page (40/60 split, per Stitch screen 3)

## 9. Phase 6 — Personal Library & Waitlist

Refs: [docs/03 §6](docs/03-api-spec.md#6-personal-library--waitlist).

- [x] `user_library` service + `/api/library` CRUD (upsert semantics, 409 `BOOK_NOT_AVAILABLE` rule)
- [x] waitlist join/leave/list + 48h expiry sweep ([docs/12 §10](docs/12-ui-design.md#10-copy-state-machine))
- [x] `/library` page: 4 KPI cards, stage tabs, `LibraryRowItem`, holds tab, AI banner deep link

## 10. Phase 7 — AI Librarian Chat

Refs: [docs/06](docs/06-ai-rag.md); HTTP shell [docs/03 §7](docs/03-api-spec.md#7-chat-sse).

- [ ] OpenRouter ACL `ChatModel` adapter + Ollama embedder ([docs/06 §5](docs/06-ai-rag.md#5-provider-configuration-acl))
- [ ] 4 tools (JSON schemas in [docs/06 §4](docs/06-ai-rag.md#4-tool-definitions)) backed by existing services
- [ ] Eino React agent loop (iter ≤ 4, 5 s tool budget, 8K window)
- [ ] `POST /api/chat` SSE writer: `meta`/`tool_call`/`tool_result`/`token`/`citations`/`done`/`error` + heartbeat
- [ ] Guardrails: rate buckets (5/min, 50/day), 40-msg cap, system prompt from [docs/06 §8](docs/06-ai-rag.md#8-guardrails)
- [ ] Conversation persistence (`conversations`/`messages`, titles from first message)
- [ ] `hooks/use-chat-stream.ts` fetch-stream parser ([docs/07 §6](docs/07-frontend.md#6-chat-streaming-client))
- [ ] `/chat` page: two panes, `AICitationCard`, meta bar, quota meters

## 11. Phase 8 — Barcode Scanner

Refs: [docs/11](docs/11-barcode-scanning.md).

- [ ] `barcode-detector` hook (`formats:["ean_13"]`, getUserMedia + cleanup) ([docs/11 §5](docs/11-barcode-scanning.md#5-nextjs-integration))
- [ ] `POST /api/books/fetch-isbn` + `pkg/checkdigit` EAN-13 validation ([docs/11 §9](docs/11-barcode-scanning.md#9-backend-validation))
- [ ] `ISBNScanner` component: overlay, torch, flip, haptics/beep, 1.5 s cooldown
- [ ] Add-Book dialog (Manual / ISBN / Scan tabs) + error handling table ([docs/11 §8](docs/11-barcode-scanning.md#8-error-handling))

## 12. Phase 9 — Admin Screens

Refs: [docs/03 §8](docs/03-api-spec.md#8-admin), [docs/07 §7 admin](docs/07-frontend.md#admin--screen-6a-stitch-3177da52).

- [ ] `GET /api/admin/data` KPIs + `GET /api/admin/copy-events` poll audit
- [ ] Admin books/copies CRUD (barcode auto-gen, `copy_events` append)
- [ ] `GET /api/admin/tree` + `GET /api/admin/shelves/{id}` inspector
- [ ] Admin users list + role change
- [ ] `/admin` KPI grid + `RealTimeAuditStream` (3 s poll)
- [ ] `/admin/layout`: `BuildingTreeBrowser`, shelf inspector, quick-assign input, SVG mini-map
- [ ] > ⚠️ TODO: verify `/admin` vs `/admin/layout` split against DESIGN.md
> ([docs/07 §3](docs/07-frontend.md#3-stitch-route-mapping)) at implementation time

## 13. Phase 10 — Polish & Verification

- [ ] Global loading/empty/error states per [docs/07 §9](docs/07-frontend.md#9-loading-empty--error-states)
- [ ] One-command demo: fresh `docker compose up --build` + seed + three protocol searches + chat turn ([docs/09 §6](docs/09-development.md#6-local-dev-workflow))
- [ ] Lint/typecheck/test suites green (`make lint`, `make test`, web build)
- [ ] > ⚠️ TODO: axe-core accessibility audit ([docs/12 §12](docs/12-ui-design.md#12-accessibility-requirements))
- [ ] Playwright E2E + CI are now in v1 scope — tracked in [Phase 13](#16-phase-13--git-workflow-and-ci)

## 14. Phase 11 — Localization

Refs: [docs/14](docs/14-localization.md).

- [x] `next-intl` setup + cookie-based locale resolution (cookie → `Accept-Language` → `en`) ([docs/14 §3–4](docs/14-localization.md#3-storage--routing))
- [x] `messages/en.json` + `messages/zh-CN.json` catalogs, key-parity test ([docs/14 §5](docs/14-localization.md#5-message-catalog))
- [ ] Externalize strings on screens 1–5 + shell; zh enum display-label maps (DONE: shell, login, enum maps; remaining screens land with their milestones) ([docs/14 §6](docs/14-localization.md#6-enum-display-label-maps))
- [x] CJK font fallback (Noto Sans SC) + `<html lang>` ([docs/14 §8](docs/14-localization.md#8-typography--fonts))
- [x] `LanguageToggle` in top nav + `/login` ([docs/14 §9](docs/14-localization.md#9-language-toggle))
- [ ] **Wayfinding i18n (bug found in review: `/books/[id]` ladder stays English in zh-CN)** — add structured `wayfinding.parts` + `copies[].location.parts` to `GET /api/books/{id}` ([docs/03 §5.2](docs/03-api-spec.md#52-get-apibooksid)); add `wayfinding.*` keys (building, floor, section, tier slug, step templates, shelfLine) to both catalogs with parity + coverage tests ([docs/14 §6.1](docs/14-localization.md#61-wayfinding-vocabulary)); compose localized ladder in `BookLocationCard`, copy manifest, `/library` shelf coordinates, catalog facet + book-page section chips, and chat citation cards; Playwright zh-CN spec asserts Chinese steps while the `MAIN/…` path token stays unchanged
- [ ] Chat `locale` field + system-prompt directive + `meta.locale` ([docs/14 §10](docs/14-localization.md#10-ai-reply-language), [docs/06 §6/§8](docs/06-ai-rag.md#6-sse-event-contract))
- [ ] zh RAG limitation recorded ([docs/06 §2](docs/06-ai-rag.md#2-rag-pipeline))

## 15. Phase 12 — Voice Input

Refs: [docs/15](docs/15-voice-input.md).

- [ ] whisper service in `docker-compose.yml` + `WHISPER_BASE_URL`/`WHISPER_MODEL` ([docs/15 §5](docs/15-voice-input.md#5-whisper-service))
- [ ] `WhisperAdapter` (ACL) + `POST /api/chat/transcribe` (auth, caps, 30s timeout, 10/min bucket, error codes) ([docs/15 §4/§6](docs/15-voice-input.md#4-transcription-endpoint))
- [ ] `hooks/use-voice-input.ts` + composer mic toggle (recording indicator, Send gating, empty/error states) ([docs/15 §8](docs/15-voice-input.md#8-frontend-wiring))
- [ ] Privacy: audio never persisted or logged ([docs/15 §7](docs/15-voice-input.md#7-privacy))

## 16. Phase 13 — Git Workflow and CI

Refs: [docs/09 §8–9](docs/09-development.md#8-testing-strategy).

- [x] `commitlint.config.js` (type/scope vocabulary) + `lefthook.yml` (`pre-commit` + `commit-msg`) ([docs/09 §9](docs/09-development.md#9-git-workflow))
- [x] First commit = scaffolding (`chore: …`); one branch per task, squash-merged ([docs/09 §9](docs/09-development.md#9-git-workflow))
- [ ] (done so far: harness, login guard, route smoke /catalog, i18n toggle) `web/playwright.config.ts` + `web/e2e/` specs: route smoke ×6 + login guard, three search modes, 409 waitlist, library tabs, chat SSE turn, admin gate, i18n toggle, voice composer ([docs/09 §8](docs/09-development.md#8-testing-strategy))
- [ ] (done: storageState auth, make e2e, real-Ollama CI) Test harness: fake ChatModel + Whisper stub via env; `storageState` auth; `make e2e` ([docs/15 §9](docs/15-voice-input.md#9-testing))
- [x] `.github/workflows/ci.yml` (lint + vet + unit + e2e); the docs/09 §8 CI `> ⚠️ TODO:` removed

## 17. Open TODO Registry (from docs)

Live registry of every `> ⚠️ TODO:` callout in the docs (re-grep after changes: `grep -n "> ⚠️ TODO" docs/*.md`):

| Location | Item | Type |
| --- | --- | --- |
| [docs/12 §7.1.1](docs/12-ui-design.md#711-stitch-mcp-tool-verification--export-runbook) | Stitch exports: Admin Portal + draft variant; 4 pending `screen.png` (FIFE failures) | exporter task |
| [docs/04](docs/04-auth.md#1-login-screen-stitch) · [docs/05 §5](docs/05-building-layout.md#5-book-detail-screen-stitch) · [docs/06 §9](docs/06-ai-rag.md#9-chat-screen-stitch) | per-screen PNG re-export + re-add embeds | exporter task |
| [docs/07 §3](docs/07-frontend.md#3-stitch-route-mapping) | confirm `/admin` vs `/admin/layout` split | verify at build |
| [docs/04 §4](docs/04-auth.md#4-wechat-placeholder) | WeChat SSO | deferred feature |
| [docs/04 §8](docs/04-auth.md#8-identity-edge-cases) | password reset · Shibboleth/SAML · WeChat | deferred features |
| [docs/10 §2](docs/10-roadmap.md#2-out-of-scope--deferred) | full deferral table (observability, TLS, RFID, fines, multi-building, email, WebSocket, roles>2, zh-TW, cloud STT, multilingual embeddings, …) | deferred |
| [docs/14 §6.1](docs/14-localization.md#61-wayfinding-vocabulary) | not a deferral: wayfinding vocabulary open task (see Phase 11) — admin-added floors/sections/tiers fall back to English until catalogued | tracked task |
| [docs/06 §2](docs/06-ai-rag.md#2-rag-pipeline) | multilingual embeddings / query translation for zh semantic search | deferred feature |
| [docs/14 §1](docs/14-localization.md#1-scope) | Traditional Chinese (`zh-TW`) + extra locales | deferred feature |
| [docs/15 §1](docs/15-voice-input.md#1-scope) | cloud/streaming STT; voice input outside `/chat` | deferred feature |
| [docs/12 §6](docs/12-ui-design.md#6-app-shell--navigation) | Stitch: regenerate chat screen for the mic + nav language toggle | exporter task |
| [docs/12 §12](docs/12-ui-design.md#12-accessibility-requirements) | axe-core audit; verify contrast ratios | polish |

## 18. Session Log

Append one row per session that touches code or docs. Keep newest at the top.

| Date (UTC) | Session / engine | Completed | Resume from | Blockers |
| --- | --- | --- | --- | --- |
| 2026-10-10 | Kilo (kilo-auto/frontier) | Milestones M0–M7 partial (M7: library+waitlist API, 48h sweeper, demo library seed, read-only /library page with KPIs/tabs/holds + leave; STILL OPEN for M7: book-detail "Add to My Library"/favorite/rating controls, 409 → waitlist dialog, banner waitlist CTA, inline row editing; M6: book detail API + /books/[id] page with wayfinding ladder, banner, manifest, neighbors; M5: tri-modal search API + /catalog UI + ⌘K + e2e vs real embeddings; Next 15.5 note: a route-level loading.tsx froze soft navigation in prod, so skeletons use in-page Suspense; M4: Next 15 web scaffold, i18n, auth guard + refresh, /login, Playwright harness w/ real-Ollama e2e DB, CI e2e job; M3: mock fixtures with docs/13 parity, ResolvePath, olbooks, cmd/seed, Ollama embedder + cmd/reindex, barcodes C01 doc fix; M2: auth backend: argon2id, JWT, register/login/refresh/logout/me, Google OIDC vs fake provider, role guards, Redis rate limiter): commitlint/lefthook, Makefile, compose, CI skeleton; Go 1.27 / Node 26 pins (docs updated); API skeleton (config, slog, middleware, error mapper, `/api/health`, Dockerfile); migrations 0001–0005 + testcontainers harness (`internal/testutil/pgtest`); `make db-migrate` verified; docs/02 index DDL fixed (double parens); compose infra up + `nomic-embed-text` pulled | Finish M7 web interactions (messages `bookActions.*`/`waitlist.*` already in catalogs), then M8 AI chat | Google SSO can only be verified against a fake `httptest` OAuth server (no Google client): manual verification pending user credentials |
| 2026-10-09 | Kilo (kilo-auto/efficient) | Grilled + documented 4 scope additions (voice input, EN/zh-CN localization, git workflow, Playwright E2E/CI); authored `docs/14-localization.md` + `docs/15-voice-input.md`; updated README/roadmap/AGENTS/TODO + cross-links (01/03/06/07/09/12, `.env.example`); added Phases 11–13 + registry/session rows | Phase 11 (localization) → Phase 12 (voice) → Phase 13 (git/CI), interleaved with Phase 1 infra | None (docs-only; no code yet) |
| 2026-10-07 | Kilo (kilo-auto/efficient) | git init; monorepo skeleton; all 14 docs; Stitch MCP discovery (project `4345961817697568978`); artifact reorg + verification pass; `TODO.md` | Phase 0 leftovers → Phase 1 infra | Stitch PNG re-export for 5 screens (FIFE auth failure) |
