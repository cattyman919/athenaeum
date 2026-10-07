# Roadmap & Scope

> v1 scope checklist and explicit deferrals for the 3-week build, the week-by-week plan, the cut list, and future enhancements — relation to features: endpoints [03-api-spec.md](./03-api-spec.md), tables [02-data-model.md](./02-data-model.md), screens [12-ui-design.md](./12-ui-design.md).

## Contents

1. [In Scope (v1) — Checklist](#1-in-scope-v1--checklist)
2. [Out of Scope — Deferred](#2-out-of-scope--deferred)
3. [Three-Week Plan](#3-three-week-plan)
4. [Cut List](#4-cut-list)
5. [Future Enhancements](#5-future-enhancements)

## 1. In Scope (v1) — Checklist

| # | Feature | Doc | Status |
| --- | --- | --- | --- |
| 1 | Email/password auth + Google SSO | [04](./04-auth.md) | ✅ in scope |
| 2 | Catalog: keyword + semantic + hybrid search | [02 §7](02-data-model.md#7-query-examples), [03 §5.1](03-api-spec.md#51-get-apibookssearch) | ✅ |
| 3 | Book detail with physical location + availability | [05](05-building-layout.md), [03 §5.2](03-api-spec.md#52-get-apibooksid) | ✅ |
| 4 | Camera barcode (EAN-13/ISBN) scanning for adding books | [11](./11-barcode-scanning.md) | ✅ |
| 5 | "Not available" 409 + waitlist | [03 §6.3](03-api-spec.md#63-waitlist), [12](12-ui-design.md#screen-3--booksid-book-detail--spatial-wayfinding) | ✅ |
| 6 | Personal library (favorite, read/unread/reading) | [03 §6](03-api-spec.md#6-personal-library--waitlist) | ✅ |
| 7 | AI chat: RAG, streaming, tool use, rate limits | [06](06-ai-rag.md) | ✅ |
| 8 | AI chat sessions (persisted conversations) | [02](02-data-model.md#conversations--messages), [06 §7](06-ai-rag.md#7-conversation-persistence) | ✅ |
| 9 | Admin UI (books, copies, layout, users) | [07 §admin](07-frontend.md#admin--screen-6a-stitch-3177da52) | ✅ |
| 10 | Mock seed + Open Library metadata fetch | [08](08-seed-data.md) | ✅ |
| 11 | Docker Compose one-command startup | [09 §2](09-development.md#2-docker-compose) | ✅ |

## 2. Out of Scope — Deferred

Every row was considered; each stays out of v1 with its reason. Silence is not a
state — unresolved questions surface as `> ⚠️ TODO:` in their owning doc.

| Deferred | Reason / condition to revisit | TODO anchor |
| --- | --- | --- |
| Observability stack (Prometheus, tracing, Grafana) | only `slog` + `tint` v1 | [09 §7](09-development.md#7-logging) |
| Production deployment topology (TLS, reverse proxy, backups) | demo runs on one machine | [01 §6](01-architecture.md#6-docker-compose-topology) |
| Self-service checkout, RFID integration | hardware dependency | [10-roadmap.md](./10-roadmap.md) (this file) |
| Loan/checkout due dates and fines | circulation policies out of demo scope ([01 §8](01-architecture.md#8-cross-cutting-rules)) | — |
| Multi-building support | single `MAIN` building ([05 §1](05-building-layout.md#1-hierarchy)) | — |
| Email delivery for waitlist notifications | in-app only ([03 §6.3](03-api-spec.md#63-waitlist)) | — |
| Password reset flow | accounts recoverable by admin | [04 §8](04-auth.md#8-identity-edge-cases) |
| WeChat SSO | button disabled placeholder | [04 §4](04-auth.md#4-wechat-placeholder) |
| Rate-limit analytics dashboard | counters exist in Redis; no UI | [03 §2](03-api-spec.md#2-rate-limiting) |
| Real-time notifications (WebSocket) | audit stream polls instead ([03 §8.1](03-api-spec.md#81-admin-kpis--audit)) | — |
| Shibboleth / SAML 2.0 SSO | broken out of demo institution | [04 §3](04-auth.md#3-google-sso) |
| Roles beyond `user`/`admin` | fixed two-role model | [04 §7](04-auth.md#7-roles--route-protection) |
| RFID gates, shelf robots, live audit streams | aspirational hardware | [12 §copy state machine](12-ui-design.md#10-copy-state-machine) |

## 3. Three-Week Plan

| Week | Deliverable | Docs touched |
| --- | --- | --- |
| 1 | Monorepo + compose up, migrations, argon2id auth + Google SSO, `cmd/seed` on mocks | [01](01-architecture.md), [02](02-data-model.md), [04](04-auth.md), [08](08-seed-data.md) |
| 2 | Catalog search (3 modes), book detail + wayfinding, personal library + waitlist, ShadCN screens 1–4 | [03](03-api-spec.md), [05](05-building-layout.md), [07](07-frontend.md), [12](12-ui-design.md) |
| 3 | AI chat (RAG + tools + SSE), barcode scanner admin intake, admin/stacks pages, polish + camera demo | [06](06-ai-rag.md), [11](11-barcode-scanning.md), [07 §admin](07-frontend.md#admin--screen-6a-stitch-3177da52) |

## 4. Cut List

Ordered cuts when the schedule slips (later items are safer to cut):

1. Hybrid mode → shipped as **semantic-only flag fallback** (mode switcher shows 2 options).
2. Floor mini-map SVG → replaced with static aisle list inside the shelf inspector.
3. Catalog era facets → sliders removed; availability + section facets remain.
4. Admin floor mini-map → replaced by a flat table of shelves in the inspector.
5. Google SSO → login falls back to email/password only; button hidden rather than broken.
6. Chat conversation list → single active session only (create new = old hidden list pruned).

Cut items **never** touch the availability/409 contract, the SSE event contract,
or the seeded fixture counts — those are demo load-bearing.

## 5. Future Enhancements

- Loan circulation at the copy status level (real `checked_out → due dates` model
  built on top of the existing state machine — [12](12-ui-design.md#10-copy-state-machine)).
- Auto-request lending from availability + waitlist notifications (still via UI events).
- Institutional SSO aggregator beyond Google (WeChat, Shibboleth Waved onto SP setup).
- Full-text relevance weights per-field and reranking (BM25 vs vector weights).
- Nightly availability crons + observed audit persistence on copy events.
- Production deploy target (Fly.io / bare metal `systemd`), env-based TLS termination.
