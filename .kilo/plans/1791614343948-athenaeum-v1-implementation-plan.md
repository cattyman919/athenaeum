# Plan: Athenaeum v1 — milestone-ordered implementation

## Goal

Build the full v1 defined by `docs/` (Phases 0–14 in `TODO.md`) from a docs-only repo
to a green, pushed, one-command demo. The docs are the contract (`AGENTS.md` precedence
order); this plan only fixes **sequencing, gates, and harness decisions** the docs leave open.

## Current state (verified)

- `main` has 2 commits (`c53e62c initial commit`, `3551555 docs: …`), remote
  `origin = git@github.com:cattyman919/athenaeum.git`. `api/` and `web/` hold only READMEs.
- `TODO.md` §2 is stale ("no commits yet") — fix in M0.
- Host tools: go, pnpm, node, docker, lefthook, gh present; **`migrate` and
  `golangci-lint` missing**.
- Stitch exports: 5 `code.html` + login PNG in `.artifacts/stitch/`; Admin Portal
  (`3177da52…`) not exported.
- Credentials: OpenRouter key available; **no Google OAuth client**.

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | Plan covers all of v1, ordered as milestones M0–M12 below (supersedes the TODO.md phase order; TODO.md checkboxes still get ticked per item). |
| 2 | Cross-cutting foundations land early: hooks + commitlint + CI skeleton in M0; next-intl + `en`/`zh-CN` catalogs in the web scaffold (M4) so every screen ships localized; Playwright harness lands with `/login` (M4) and every later user-facing milestone adds its spec. Voice stays after chat. |
| 3 | Git: one branch per TODO task → local `git merge --squash` → one Conventional Commit on `main`; **push `main` at each milestone gate** so GitHub Actions validates it. No force-push. |
| 4 | Google SSO is fully built but verified only via `httptest` fake OAuth server; manual verification logged as a blocker in TODO.md Session Log for the user. |
| 5 | Real OpenRouter used for a manual chat check in M8/M12; E2E always uses the fake ChatModel stub. |
| 6 | CI E2E runs **real Ollama** with `nomic-embed-text` pulled and cached (`actions/cache` on the ollama volume dir); semantic E2E asserts real relevance. Chat + Whisper are stubbed per docs/09 §8.1. |
| 7 | Missing host tools: Makefile runs migrations via `go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.x`; golangci-lint installed via `brew`/`go install` (documented prereq, pinned in CI via `golangci/golangci-lint-action`). |
| 8 | Version pins follow docs (latest Go 1.27 and Node 26 per user decision; Next.js 15 / React 19, Postgres 16, Redis 7, pnpm 9). Tailwind **v3.4** (docs require `tailwind.config.ts`). If a required dependency (e.g. Eino, pgx, x/ deps) needs a newer Go, bump the pin in `docs/01`, `docs/09`, `docs/README.md` §2, `TODO.md` §2, `api/Dockerfile`, CI in **one** `build:` commit — never silently. |
| 9 | E2E stub providers live in a single Go binary `api/cmd/e2e-stubs` (OpenAI-compatible streaming `/chat/completions` scripted per docs/06 §6 + Whisper `/asr` fixed text). Record the path in docs/09 §8.1 in the same commit. |
| 10 | Admin visuals: in M11 retry Stitch export of screen `3177da52…` via Stitch MCP (`stitch_get_screen`); if it fails, build from docs/07 §7 + docs/12 specs and keep the registry TODO open. Resolve the `/admin` vs `/admin/layout` TODO at that point. |
| 11 | Scanner (M13, added 2026-10-15): one `BarcodeScannerDialog` with Patron Lookup / Staff Intake; patron endpoints `POST /api/books/scan` + `POST /api/copies/{id}/return`; self-service **return** is in v1 but self-service checkout + loans/RFID/label/MARC stay deferred (docs/10 §2). `fetch-isbn` is 👑 admin-only. |

## Milestones

Each task = one branch + one squash commit (scope vocabulary from docs/09 §9). Every
milestone ends with the **gate** below.

### M0 — Repo foundations (TODO Phase 0 + part of 13)
1. `chore`: fix TODO.md §2 state (commits exist, remote set, milestone order = this plan); commit `.kilo/plans/` as project artifacts.
2. `build`: root `package.json` (private, devDeps `@commitlint/cli`, `@commitlint/config-conventional`), `commitlint.config.js` (types/scopes from docs/09 §9), `lefthook.yml` (`pre-commit`: gofmt+golangci-lint on `api/**`, eslint+prettier on `web/**`, both skip when dirs have no code; `commit-msg`: commitlint). `lefthook install`.
3. `build`: root `Makefile` with every target in docs/09 §5 (targets for not-yet-existing parts fail with a clear message rather than being omitted).
4. `build(infra)`: `docker-compose.yml` verbatim from docs/09 §2 (api/web services under a profile or commented build until M1/M4 Dockerfiles exist — then enabled).
5. `ci`: `.github/workflows/ci.yml` skeleton — Go job (gofmt check, vet, golangci-lint, `go test ./...`) and web job (lint, vitest, build), each no-op-safe until code exists. E2E job added in M4.

### M1 — API skeleton + DB (Phase 1)
1. `build(api)`: `api/go.mod` + deps from TODO Phase 1; `api/Dockerfile` (docs/09 §3).
2. `feat(api)`: `internal/config` (caarlos0/env, every var in docs/01 §7, prod requires 32-byte JWT secrets), slog/tint per docs/09 §7, request-id middleware, CORS, graceful shutdown (docs/01 §8).
3. `feat(db)`: migrations — DDL **verbatim** from docs/02 §4, indexes §5, `book_locations` view §6, `updated_at` trigger. Up + down files.
4. `feat(api)`: typed domain errors in `model`, single error mapper → envelope (docs/03 §1, §9); `GET /api/health`.
5. Tests: config table tests; migration up/down + view smoke via testcontainers (`pgvector/pgvector:pg16`); handler test for health + error envelope.

### M2 — Auth backend (Phase 2, API half)
1. `feat(api)`: `pkg/hash` argon2id (docs/04 §6), `pkg/jwt` (docs/04 §5) — table tests.
2. `feat(api)`: register/login/me/logout, refresh rotation + `REUSED_TOKEN` replay detection, cookies exactly per docs/04.
3. `feat(api)`: Google SSO redirect + callback with state cookie; tested against an `httptest` OAuth server.
4. `feat(api)`: `RequireAuth`/`RequireAdmin`; Redis rate limiter with every bucket in docs/03 §2 + `X-RateLimit-*` headers.
5. Tests: service fakes, handler envelope per error code, role guards, limiter (miniredis or real redis).

### M3 — Seed data & embeddings (Phase 3)
1. `feat(db)`: `api/mocks/building.json` + `books.json` with **exact** docs/13 values.
2. `feat(api)`: `service/building.ResolvePath` (docs/05 §4) + tests.
3. `feat(api)`: `cmd/seed` (users → tree → books → copies, barcode counter docs/08 §5, deterministic, idempotent, `--reset`) + failure-mode tests (docs/08 §8). Verify parity: 60 copies / 12 `checked_out` / 5 zero-copy books.
4. `feat(api)`: `service/olbooks` Open Library client + fallbacks + `OFFLINE_MODE` (docs/08 §6).
5. `feat(api)`: Ollama embedder (ACL) + `cmd/reindex` chunker v1 (~300 tok, 50 overlap) + batch upsert (docs/06 §2).

### M4 — Web foundation + login + E2E harness (Phase 2 web half, 11, 13)
1. `build(web)`: Next.js 15 + TS + pnpm 9, Tailwind 3.4, ShadCN, tokens from docs/12 §3, Lucide, Vitest, ESLint/Prettier; `web/Dockerfile`; `/api/*` rewrite to `API_BASE_URL` (docs/01 §6). Enable api+web in compose.
2. `feat(i18n)`: next-intl without routing, `web/i18n/request.ts` (cookie `athenaeum_lang` → `Accept-Language` → `en`), `messages/en.json` + `zh-CN.json`, key-parity + locale-resolution Vitest, enum label maps scaffold, Noto Sans SC fallback, `<html lang>`, `LanguageToggle` (docs/14).
3. `feat(web)`: `lib/api-server.ts` cookie forwarding, `middleware.ts` guard, AuthProvider + refresh-retry (docs/07 §4), app shell/top nav (docs/12 §6).
4. `feat(web)`: `/login` per Stitch screen 1 (role tabs, disabled WeChat, no SAML row), localized.
5. `test(web)`: `playwright.config.ts` (Chromium), global setup capturing `storageState` for admin + demo patron, specs: login guard redirect, i18n toggle on `/login`.
6. `ci`: E2E job — compose up (postgres, redis, ollama w/ cached model, api, web, e2e-stubs once it exists), migrate, seed, reindex, `pnpm exec playwright test`, upload traces on failure; `OFFLINE_MODE=true`, no secrets. `make e2e` mirrors it.

### M5 — Catalog & search (Phase 4)
Backend: keyword (tsvector), semantic (pgvector top-20), hybrid (RRF k=60), facets, pagination envelope, `availability.state`, `GET /api/books/search` (30/min). Web: `/catalog` (tri-modal switcher, facet sidebar, BookCard grid, skeletons, URL-as-state) and ⌘K `CommandPalette`. E2E: three search modes with relevance assertions from docs/13; route smoke for `/catalog`.

### M6 — Book detail & wayfinding (Phase 5)
`GET /api/books/{id}` (copies + `book_locations` + neighbors), `BookLocationCard` ladder (`MAIN/3F/SF/A-1/S-2`), tri-state availability banner, `/books/[id]` 40/60 split. E2E: detail smoke + wayfinding text. **Amendment:** wayfinding prose must be localized (zh-CN) from structured `wayfinding.parts` via the vocabulary in docs/14 §6.1 — tracked in TODO.md Phase 11 as an open fix.

### M7 — Personal library & waitlist (Phase 6)
`/api/library` CRUD (upsert, 409 `BOOK_NOT_AVAILABLE`), waitlist join/leave/list + 48h expiry sweep, `/library` (KPI cards, stage tabs, holds tab, AI banner). E2E: 409 → waitlist journey on a zero-copy book; library tabs.

### M8 — AI librarian chat (Phase 7 + chat locale from 11)
1. `feat(chat)`: OpenRouter `ChatModel` ACL adapter; 4 tools (docs/06 §4) on existing services; Eino React agent (≤4 iters, 5 s tool budget, 8K window).
2. `feat(chat)`: `POST /api/chat` SSE writer (event order per docs/06 §6, heartbeat), guardrails (5/min, 50/day, 40-msg cap, system prompt docs/06 §8), `locale` field → prompt directive + `meta.locale`, conversation persistence + titles.
3. `test(chat)`: fake Eino stream tests asserting SSE order; `api/cmd/e2e-stubs` chat endpoint.
4. `feat(web)`: `hooks/use-chat-stream.ts`, `/chat` two-pane, `AICitationCard`, meta bar, quota meters.
5. E2E: chat SSE turn against stub. Manual: one real OpenRouter turn in en + zh-CN.

### M9 — Voice input (Phase 12)
Whisper service in compose, `WhisperAdapter` + `POST /api/chat/transcribe` (auth, caps, 30 s timeout, 10/min, error codes, audio never persisted/logged — docs/15), Whisper stub in `e2e-stubs`, `hooks/use-voice-input.ts` + composer mic. E2E: voice composer with fake media-device flags.

### M10 — Barcode scanner (Phase 8)
`pkg/checkdigit` EAN-13 + `POST /api/books/fetch-isbn` (Redis cache, Open Library), `barcode-detector` hook, `ISBNScanner`, Add-Book dialog (Manual/ISBN/Scan) + error table (docs/11 §8). E2E: ISBN tab manual entry path (camera path unit-tested only).

### M11 — Admin (Phase 9)
Retry Stitch admin export (decision 10). `GET /api/admin/data`, `copy-events` poll, books/copies CRUD with barcode auto-gen + `copy_events`, tree + shelf inspector, users + role change. Web `/admin` KPI grid + `RealTimeAuditStream` (3 s poll), `/admin/layout` tree browser, inspector, quick-assign, SVG mini-map. Admin stays English. E2E: admin role gate (patron blocked), admin smoke.

### M12 — Polish & verification (Phase 10)
Loading/empty/error states (docs/07 §9); full one-command demo from clean volumes (`docker compose up --build` → migrate → seed → reindex → three searches → chat turn); manual bilingual QA checklist; README quickstart check; TODO.md registry re-grep (`grep -n "> ⚠️ TODO" docs/*.md`). axe-core audit stays ⚠️ TODO.

### M13 — Patron barcode scanner (Phase 14; scope added 2026-10-15)
Docs re-scoped in docs/11 + docs/03 §5.4/§6.4 (v1 item 16). One dialog, two audiences; artifact `.artifacts/stitch/barscanner_ISBN/code.html`.
1. `feat(api)`: `POST /api/books/scan` — local resolver (ISBN-13 → `books` by `isbn13`; `ATH-…-CYY` → `book_copies`), reusing `pkg/checkdigit`; `200 in_catalog:false` when validated but not local; service + handler tests.
2. `feat(api)`: `POST /api/copies/{id}/return` — `checked_out → available` (or `reserved_waitlist` + oldest-hold turnover per docs/12 §10); appends `copy_events` (`source='self_return'`, actor = caller); 409 `CONFLICT` when not `checked_out`.
3. `feat(api)`: rate-limit buckets (scan 30/1min, return 10/1min) + endpoint index + error catalog.
4. `feat(web)`: `BarcodeScannerDialog` (Patron Lookup / Staff Intake, `formats:['ean_13','code_128']`, manual code fallback) + `hooks/use-barcode-scanner.ts`; nav **Scan** button (all users) + `/library` "Scan to return" + `/admin` Staff Intake entry; patron result card (record + ladder + other-copy availability + waitlist CTA) and copy result (**Return this copy**).
5. `feat(i18n)`: `scanner.*` keys in both catalogs (parity); Staff Intake stays English (docs/14 §1).
6. `test`: Go resolver + return state-machine + waitlist-turnover tests; Playwright patron ISBN lookup + self-service return via the manual code path (camera path unit-only).
Gate: the docs/11 §11 deferral (RFID encode / thermal label print / Edit-MARC) stays unbuilt.

### M14 — Auto-index worker + detail rating (decisions 2026-10-16; docs/06 §2, docs/08 §7)
1. `feat(db)`: migration 0006 — `books.embedding_dirty BOOL NOT NULL DEFAULT false` + partial index `WHERE embedding_dirty`; canonical DDL already in docs/02 §4.
2. `feat(api)`: admin repo — `CreateBook` sets dirty=true; `PatchBook` sets dirty=true only when title/authors/description change; worker clears on success.
3. `feat(api)`: `service/index` worker — dirty set is the union of `embedding_dirty` **and** books with **zero embedding rows** (gap 1a: fresh-start backfill, so a freshly seeded catalog self-heals without `make reindex`); wake-up via notify (non-blocking send from admin service) + 5s poll fallback; per-book `Indexer.Run(bookID)`; **waits for Ollama** — down or model-not-pulled keeps the book queued and retries with backoff instead of failing (startup backfill and later edits both wait); slog lines (start/success/failure + pending count). Wired in `cmd/api/main.go` like `RunSweeper`.
4. `feat(api)`: detail `my_library_entry` gains nullable `rating` (model + repo SELECT + docs/03 §5.2 example already updated); web reads it already.
5. `test`: migration up/down; dirty-flag unit tests (RAG-field vs non-RAG patch); worker test with fake embedder (dirty cleared, failure keeps dirty, zero-embedding books picked up, Ollama-down retries); rating detail test.
Gate: `make lint` + `make test` + `make e2e` green; `cmd/seed` still leaves dirty=false but the worker backfills zero-embedding books on boot (docs 06/08/09/README quickstart no longer require `make reindex` for a fresh start).

## Milestone gate (every milestone)

1. `make lint` and `make test` green; `make e2e` green from M4 onward.
2. TODO.md checkboxes ticked only for items meeting the "Complete" rule; new discoveries added as tasks, not code.
3. Any doc deviation (version bump, stub path, admin route split) edited in the owning doc in the same commit.
4. Squash commits on `main`, `git push origin main`, confirm CI green with `gh run list`/`gh run view`.
5. Append a TODO.md Session Log row (completed, "resume from", blockers — incl. Google SSO manual check).

## Risks / failure modes

- **Go pin vs dependency minimums** → decision 8 (one explicit bump commit).
- **Ollama in CI** (download size, CPU embedding time) → cache model dir; reindex only the 40 fixtures; if CI time is unacceptable, revisit decision 6 by asking the user (do not silently switch to a stub).
- **Whisper image on arm64/Apple Silicon** → only needed locally for manual voice checks; CI uses the stub.
- **Eino API drift** → consult current docs (Context7) before writing the agent; keep it behind `service/ai` interfaces.
- **Stitch admin export failure** → decision 10 fallback.
- **Scope creep** → every `> ⚠️ TODO:` deferral (WeChat, password reset, zh-TW, multilingual embeddings, cross-browser E2E, etc.) stays unimplemented.

## Out of scope

Everything in docs/10 §2; Stitch PNG re-exports for screens 2–5 (exporter task, not blocking); changelog automation; deployment beyond local compose.

## Validation (end state)

- Fresh clone → `cp .env.example .env` → `make up && make db-migrate && make seed && make reindex` → `make api` + `make web` → log in as `demo@athenaeum.dev` / `athenaeum-demo`, run keyword/semantic/hybrid searches, open a book's wayfinding, hit the 409 waitlist, chat (stub and real), toggle zh-CN, dictate via mic, scan/enter an ISBN as a patron (lookup + return) and as an admin (staff intake), manage layout.
- `make ci` green locally; GitHub Actions green on `main`; seed parity counts hold.
