# API Specification

> Canonical HTTP contract for the Athenaeum Go API: every endpoint with request/response JSON, error envelope, pagination, filtering, rate limits, and the SSE chat stream. Handlers in [01-architecture.md](./01-architecture.md), tables in [02-data-model.md](./02-data-model.md).

## Contents

1. [Conventions](#1-conventions)
2. [Rate Limiting](#2-rate-limiting)
3. [Endpoint Index](#3-endpoint-index)
4. [Auth Endpoints](#4-auth-endpoints)
5. [Books & Search](#5-books--search)
6. [Personal Library, Waitlist & Loans](#6-personal-library-waitlist--loans)
7. [Chat (SSE)](#7-chat-sse)
8. [Admin](#8-admin)
9. [Error Catalog](#9-error-catalog)
10. [Traceability](#10-traceability)

## 1. Conventions

| Rule | Value |
| --- | --- |
| Base URL | `http://localhost:8080` (dev), `/api` via Next proxy in compose |
| Auth | httpOnly cookies `athenaeum_at` (15 min JWT) + `athenaeum_rt` (7-day opaque) — [04-auth.md](./04-auth.md#5-token-model) |
| Content | `application/json; charset=utf-8` (except SSE) |
| Success envelope | single-object endpoints return the resource; list endpoints return `{ "items":[…], "meta":{…} }` |
| Pagination | `?page=1&per_page=20` (max 50) → `"meta":{"page":1,"per_page":20,"total":137,"total_pages":7}` |
| Filtering | repeated `section=` params acceptable; see per-endpoint param tables |
| Rate limit headers | `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` (epoch s) on every response |
| Traceability | `X-Request-Id` response header (UUID, from slog middleware) |
| Sorting | `sort=relevance` (default: search), `<field>:asc\|desc` for admin lists |

### Success envelope

```json
{ "items": "[ ...resource array... ]", "meta": { "page": 1, "per_page": 20, "total": 137, "total_pages": 7 } }
```

### Error envelope

`error` is always the machine code; `message` is user-displayable; extra keys per code.

```json
{
  "error": "BOOK_NOT_AVAILABLE",
  "message": "This book is not currently available in the building.",
  "suggested_actions": ["reserve", "notify_when_available"]
}
```

## 2. Rate Limiting

Redis fixed-window counters (`INCR` + `EXPIRE`), peer = `user_id` when authenticated
else IP (hashed). Middleware: [01-architecture.md](01-architecture.md#3-request-lifecycle).

| Scope | Limit | Window | Key | On breach |
| --- | --- | --- | --- | --- |
| `POST /api/auth/login` | 5 | 15 min | ip | 429 (+`Retry-After`) |
| `POST /api/auth/register` | 5 | 15 min | ip | 429 |
| All other `/api/*` | 120 | 1 min | user_id xor ip | 429 |
| `GET /api/books/search` | 30 | 1 min | user_id | 429 `RATE_LIMITED` |
| `POST /api/chat` | 5 | 1 min | user_id | 429 |
| `POST /api/chat` | 50 | 24 h | user_id | 429 (surfaced in UI quota meter — [07](./07-frontend.md#7-page-breakdowns)) |
| `POST /api/chat/transcribe` | 10 | 1 min | user_id | 429 (dedicated bucket; CPU-expensive — [15 §6](./15-voice-input.md#6-limits-rate--errors)) |
| `POST /api/chat/transcribe?partial=true` | 60 | 1 min | user_id | 429 (live partials; own bucket, never drains the 10/min one; plus **1 in flight per user** → 429 `RATE_LIMITED` with `Retry-After: 1` — [15 §6](./15-voice-input.md#6-limits-rate--errors)) |
| `POST /api/books/fetch-isbn` | 10 | 1 min | user_id | 429 (admin intake) |
| `POST /api/books/scan` | 30 | 1 min | user_id | 429 (`RATE_LIMITED`; patron ISBN / copy-tag lookup — [11 §9](./11-barcode-scanning.md#9-endpoints--validation)) |
| `POST /api/copies/{id}/return` | 10 | 1 min | user_id | 429 (`RATE_LIMITED`; patron self-service return — [11 §9](./11-barcode-scanning.md#9-endpoints--validation)) |
| `POST /api/copies/{id}/borrow` | 10 | 1 min | user_id | 429 (`RATE_LIMITED`; patron self-service checkout — [11 §9](./11-barcode-scanning.md#9-endpoints--validation)) |
| `POST /api/copies/{id}/renew` | 5 | 1 min | user_id | 429 (`RATE_LIMITED`; one renewal per loan) |
| `POST /api/books/identify-isbn` | 10 | 1 min | user_id | 429 (`RATE_LIMITED`; patron OL identification — [§5.5](#55-post-apibooksidentify-isbn)) |
| `POST /api/books/{id}/borrow` | 10 | 1 min | user_id | 429 (`RATE_LIMITED`; borrow-by-book shares the `copies/borrow` bucket — [§6.8](#68-post-apibooksidborrow)) |
| `POST /api/books/{id}/return` | 10 | 1 min | user_id | 429 (`RATE_LIMITED`; return-by-book shares the `copies/return` bucket — [§6.9](#69-post-apibooksidreturn)) |
| `GET /api/books/{id}/cover` | — | — | — | not limited: a catalog page loads dozens of covers, each one indexed row read ([§5.6](#56-get-apibooksidcover)) |

Specific limits shadow the general one (most restrictive wins per endpoint).

## 3. Endpoint Index

Auth column: 🔓 public · 🔒 `user` · 👑 `admin`.

| # | Method | Path | Auth | Purpose | Details |
| --- | --- | --- | --- | --- | --- |
| 1 | GET | `/api/health` | 🔓 | liveness | [§4.1](#41-get-aphealth) |
| 2 | POST | `/api/auth/register` | 🔓 | create account | [§4.2](#42-post-apiauthregister) |
| 3 | POST | `/api/auth/login` | 🔓 | email/password login | [§4.3](#43-post-apiauthlogin) |
| 4 | GET | `/api/auth/google` | 🔓 | begin SSO (302) | [§4.5](#45-google-sso) |
| 5 | GET | `/api/auth/google/callback` | 🔓 | SSO callback | [§4.5](#45-google-sso) |
| 6 | POST | `/api/auth/refresh` | 🔓 (needs `athenaeum_rt`) | rotate session | [§4.4](#44-post-apiauthrefresh) |
| 7 | POST | `/api/auth/logout` | 🔒 | clear + revoke | [§4.4](#44-post-apiauthrefresh) |
| 8 | GET | `/api/auth/me` | 🔒 | current user | [§4.6](#46-get-apiauthme) |
| 9 | GET | `/api/books/search` | 🔒 | tri-modal search | [§5.1](#51-get-apibookssearch) |
| 10 | GET | `/api/books/{id}` | 🔒 | detail + copies + wayfinding | [§5.2](#52-get-apibooksid) |
| 11 | POST | `/api/books/fetch-isbn` | 👑 | admin ISBN intake metadata | [§5.3](#53-post-apibooksfetch-isbn) |
| 12 | POST | `/api/books/{id}/waitlist` | 🔒 | join waitlist | [§6.3](#63-waitlist) |
| 13 | DELETE | `/api/books/{id}/waitlist` | 🔒 | leave waitlist | [§6.3](#63-waitlist) |
| 14 | GET | `/api/waitlist` | 🔒 | my active holds | [§6.3](#63-waitlist) |
| 15 | GET | `/api/library` | 🔒 | personal library list | [§6.1](#61-get-apilibrary) |
| 16 | POST | `/api/library` | 🔒 | add book entry | [§6.2](#62-addupdate-library-entry) |
| 17 | PATCH | `/api/library/{book_id}` | 🔒 | update status/progress/rating | [§6.2](#62-addupdate-library-entry) |
| 18 | DELETE | `/api/library/{book_id}` | 🔒 | remove entry | [§6.2](#62-addupdate-library-entry) |
| 19 | GET | `/api/chat/conversations` | 🔒 | list chats | [§7](#7-chat-sse) |
| 20 | POST | `/api/chat/conversations` | 🔒 | create chat | [§7](#7-chat-sse) |
| 21 | GET | `/api/chat/conversations/{id}` | 🔒 | fetch messages | [§7](#7-chat-sse) |
| 22 | DELETE | `/api/chat/conversations/{id}` | 🔒 | delete chat | [§7](#7-chat-sse) |
| 23 | POST | `/api/chat` | 🔒 | **SSE** stream reply | [§7.1](#71-sse-stream) |
| 24 | POST | `/api/chat/transcribe` | 🔒 | voice → transcript | [§7.2](#72-post-apichattranscribe) |
| 25 | GET | `/api/admin/data` | 👑 | KPI snapshot | [§8.1](#81-admin-kpis--audit) |
| 26 | GET | `/api/admin/books` | 👑 | admin book list | [§8.2](#82-admin-books) |
| 27 | POST | `/api/admin/books` | 👑 | create book (+copies) | [§8.2](#82-admin-books) |
| 28 | PATCH | `/api/admin/books/{id}` | 👑 | edit book | [§8.2](#82-admin-books) |
| 29 | DELETE | `/api/admin/books/{id}` | 👑 | remove book | [§8.2](#82-admin-books) |
| 30 | POST | `/api/admin/copies` | 👑 | add copy | [§8.3](#83-admin-copies) |
| 31 | PATCH | `/api/admin/copies/{id}` | 👑 | status/shelf mutation | [§8.3](#83-admin-copies) |
| 32 | DELETE | `/api/admin/copies/{id}` | 👑 | remove copy | [§8.3](#83-admin-copies) |
| 33 | GET | `/api/admin/tree` | 👑 | building hierarchy json | [§8.4](#84-admin-tree--building) |
| 34 | GET | `/api/admin/shelves/{id}` | 👑 | shelf manifest inspector | [§8.4](#84-admin-tree--building) |
| 35 | GET | `/api/admin/copy-events` | 👑 | audit stream (poll) | [§8.1](#81-admin-kpis--audit) |
| 36 | GET | `/api/admin/users` | 👑 | user list | [§8.5](#85-admin-users) |
| 37 | PATCH | `/api/admin/users/{id}` | 👑 | change role | [§8.5](#85-admin-users) |
| 38 | POST | `/api/books/scan` | 🔒 | resolve scanned ISBN / copy tag | [§5.4](#54-post-apibooksscan) |
| 39 | POST | `/api/copies/{id}/return` | 🔒 | self-service return | [§6.4](#64-post-apicopiesidreturn) |
| 40 | GET | `/api/admin/index-status` | 👑 | auto-index backlog | [§8.1](#81-admin-kpis--audit) |
| 41 | POST | `/api/copies/{id}/borrow` | 🔒 | self-service checkout (loan) | [§6.5](#65-post-apicopiesidborrow) |
| 42 | POST | `/api/copies/{id}/renew` | 🔒 | renew an open loan | [§6.6](#66-post-apicopiesidrenew) |
| 43 | GET | `/api/loans` | 🔒 | my loans (active + history) | [§6.7](#67-get-apiloans) |
| 44 | POST | `/api/books/identify-isbn` | 🔒 | patron ISBN identification via Open Library | [§5.5](#55-post-apibooksidentify-isbn) |
| 45 | POST | `/api/books/{id}/borrow` | 🔒 | borrow by book (server picks copy) | [§6.8](#68-post-apibooksidborrow) |
| 46 | POST | `/api/books/{id}/return` | 🔒 | return by book (closes caller's oldest open loan) | [§6.9](#69-post-apibooksidreturn) |
| 47 | POST | `/api/admin/books/{id}/isbns` | 👑 | attach an edition ISBN to a book | [§8.6](#86-admin-isbn-management--merge) |
| 48 | DELETE | `/api/admin/books/{id}/isbns/{isbn13}` | 👑 | detach a non-primary edition ISBN | [§8.6](#86-admin-isbn-management--merge) |
| 49 | POST | `/api/admin/books/{id}/merge` | 👑 | merge source book into target (one tx) | [§8.6](#86-admin-isbn-management--merge) |
| 50 | POST | `/api/admin/books/bulk-delete` | 👑 | atomically remove selected books | [§8.2](#82-admin-books) |
| 51 | GET | `/api/books/{id}/cover` | 🔓 | cover image from the Postgres cache | [§5.6](#56-get-apibooksidcover) |

404 for unknown `id` values on every `/api/.../{id}` route.

## 4. Auth Endpoints

Sequences and cookie mechanics: [04-auth.md](./04-auth.md). WeChat button is a
disabled placeholder ([04-auth.md](./04-auth.md#4-wechat-placeholder), [12-ui-design.md](./12-ui-design.md#screen-1--login-institution-gateway)).

### 4.1 GET /api/health

```json
{ "status": "ok", "version": "v1" }
```

### 4.2 POST /api/auth/register

```json
// request
{ "name": "Dana Reader", "email": "dana@athenaeum.dev", "password": "correct horse battery" }
// response 201
{ "user": { "id": "9d2a…", "email": "dana@athenaeum.dev", "name": "Dana Reader", "role": "user" } }
```

- Validation: email RFC-ish + password ≥ 10 chars → 400 `VALIDATION_FAILED`;
  duplicate email (case-insensitive) → 409 `EMAIL_TAKEN`.
- Sets fresh session cookies (auto-login).

### 4.3 POST /api/auth/login

```json
// request
{ "email": "demo@athenaeum.dev", "password": "athenaeum-demo" }
// response 200 — body + Set-Cookie athenaeum_at, athenaeum_rt ([04](./04-auth.md))
{ "user": { "id": "0f51…", "email": "demo@athenaeum.dev", "name": "Demo Patron", "role": "user" } }
```

- `401 INVALID_CREDENTIALS` (identical message for unknown email vs bad password).
- IP rate limit 5/15min ([§2](#2-rate-limiting)).

### 4.4 POST /api/auth/refresh · POST /api/auth/logout

- refresh: rotate `athenaeum_rt` (server marks old row revoked, issues new pair).
  Replayed/revoked token → 401 `REUSED_TOKEN` + entire session family revoked
  (defense in depth — [04-auth.md](./04-auth.md#5-token-model)).
- logout: 204; revokes all tokens for the session and clears both cookies.

### 4.5 Google SSO

- `GET /api/auth/google` → 302 `accounts.google.com/o/oauth2/v2/auth?...&state=<signed>` (`state` 10-min cookie).
- `GET /api/auth/google/callback?code=…&state=…` → code exchange, user upsert, session cookies → 302 to `web` `next` param (`/catalog` default).
- Mismatch/failure → 302 back to `/login?error=sso`.

### 4.6 GET /api/auth/me

Same shape as `GET /api/auth/login` `user` (401 when `athenaeum_at` expired without valid refresh handling client-side refresh — [07](./07-frontend.md#4-auth-context--guards)).

## 5. Books & Search

### 5.1 GET /api/books/search

Keyed 30/1min per user ([§2](#2-rate-limiting)). Powered by
[02 §7 Query Examples](02-data-model.md#7-query-examples).

Query params:

| Param | Values (default) | Notes |
| --- | --- | --- |
| `q` | free text | `""`/absent = **browse** (keyword listing); required for explicit `semantic`/`hybrid` |
| `mode` | `keyword` \| `semantic` \| `hybrid` (default `hybrid`) | hybrid = RRF k=60 (default since 2026-10-16, [07](07-frontend.md#catalog--screen-2-stitch-e9f24637)) |
| `section` | section code, repeatable (`SF`, `SCI`, `HVA`, `REF`, `PER` — [05](./05-building-layout.md#2-section-categories)) | facet |
| `availability` | `any` \| `available` (default `any`) | "Only Available in Building" sidebar flag |
| `era` | `pre1800`, `1800_1970`, `1970_2000`, `2000_n` (repeatable) | publication buckets |
| `sort` | `relevance` \| `title` \| `year:asc` \| `year:desc` | relevance only meaningful for search results |
| `page`, `per_page` | pagination | `per_page` ≤ 50 |

Response 200 (`items`), each item:

```json
{
  "items": [
    {
      "id": "8c14…",
      "title": "Dune",
      "authors": ["Frank Herbert"],
      "isbn13": "9780441013593",
      "cover_url": "https://covers.openlibrary.org/b/isbn/9780441013593-L.jpg",
      "section": { "code": "SF", "name": "Science Fiction & Speculative", "color": "#3B82F6" },
      "location": { "path": "MAIN/3F/SF/A-1/S-2", "shelf_code": "S-2" },
      "availability": {
        "total_copies": 3,
        "available_copies": 2,
        "state": "available",
        "soon": null
      },
      "published_year": 1965,
      "rating_avg": 4.6,
      "rating_count": 12,
      "score": 0.874
    }
  ],
  "meta": { "page": 1, "per_page": 20, "total": 40, "total_pages": 2,
            "index_pending": 0, "mode_used": "hybrid", "degraded": false }
}
```

`meta.mode_used` (`keyword`\|`semantic`\|`hybrid`) reports the mode that **actually** ran,
and `meta.degraded` (bool) is `true` when a requested `semantic`/`hybrid` fell back to
`keyword` because embeddings were unavailable (docs/06 §2; the fallback returns 200 with
keyword results rather than 503 — decided 2026-10-16, so a default-hybrid UI never errors on
an Ollama outage). An **empty `q`** always browses (keyword listing, no embedding call),
whatever `mode` is requested.

`rating_avg` (1 decimal) + `rating_count` come from the [`book_ratings` view](02-data-model.md#62-book_ratings);
both are `null` when the book has no ratings. Same fields on the detail schema ([§5.2](#52-get-apibooksid))
and the catalog cards ([07](07-frontend.md#catalog--screen-2-stitch-e9f24637)).

`meta.index_pending` (int, every `mode`, additive to the standard [§1](#1-conventions)
meta) is the auto-index backlog size — the same count as `pending_count` of
[`GET /api/admin/index-status`](#81-admin-kpis--audit): books whose embeddings are
not yet current, so they may be missing from `semantic`/`hybrid` results until the
worker ([06 §2](06-ai-rag.md#2-rag-pipeline)) catches up. `0` when fully indexed.

`availability.state` resolution: `available` when `available_copies > 0`;
`not_in_building` when `total_copies == 0`; otherwise the status shared by all
copies, or `checked_out` if they diverge (degradation rule — banner colors from
[12-ui-design.md](./12-ui-design.md#screen-3--booksid-book-detail--spatial-wayfinding)).

### 5.2 GET /api/books/{id}

Assembles detail page ([07 §5.3](./07-frontend.md#booksid--screen-3-stitch-723960ca), Stitch screen
`723960ca…`).

Response 200:

```json
{
  "id": "8c14…",
  "title": "Dune",
  "subtitle": null,
  "authors": ["Frank Herbert"],
  "isbn13": "9780441013593",
  "isbn10": "0441013595",
  "publisher": "Ace Books",
  "published_year": 1965,
  "description": "…",
  "dewey_num": "813.54",
  "loc_num": "PS3553.I57 D8 1965",
  "cover_url": "…",
  "rating_avg": 4.6,
  "rating_count": 12,
  "isbns": [
    { "isbn13": "9780593135204", "is_primary": true },
    { "isbn13": "9780593355275", "is_primary": false }
  ],
  "section": { "code": "SF", "name": "Science Fiction & Speculative", "color": "#3B82F6" },
  "availability": { "total_copies": 3, "available_copies": 2, "state": "available" },
  "copies": [
    {
      "id": "b1e3…",
      "barcode": "ATH-002941-C01",
      "status": "available",
      "condition": "good",
      "last_state_change": "2026-10-01T09:15:00Z",
      "location": { "path": "MAIN/3F/SF/A-1/S-2", "ladder": "Athenaeum Main Building (North Stacks) → Floor 3 → Science Fiction & Speculative → Aisle A-1 → Shelf S-2 (Row 4, Eye Level)", "parts": { "building_code": "MAIN", "building_name": "Athenaeum Main Building", "floor_number": 3, "floor_name": "North Stacks", "section_code": "SF", "section_name": "Science Fiction & Speculative", "aisle_code": "A-1", "shelf_code": "S-2", "row_level": 4, "height_tier": "Eye Level" } }
    }
  ],
  "wayfinding": { "path": "MAIN/3F/SF/A-1/S-2", "parts": { "building_code": "MAIN", "building_name": "Athenaeum Main Building", "floor_number": 3, "floor_name": "North Stacks", "section_code": "SF", "section_name": "Science Fiction & Speculative", "aisle_code": "A-1", "shelf_code": "S-2", "row_level": 4, "height_tier": "Eye Level" }, "ladder_steps": ["Floor 3 — North Stacks", "Section: Science Fiction & Speculative", "Aisle A-1", "Shelf S-2 — Row 4, Eye Level"] },
  "neighbors": [
    { "id": "b3aa…", "title": "Hyperion", "authors": ["Dan Simmons"], "cover_url": "…" }
  ],
  "my_library_entry": { "status": "reading", "is_favorite": false, "rating": null }
}
```

- `my_library_entry` carries the caller's `user_library` state: `status`, `is_favorite`, and `rating` (1–5 or `null`; decided 2026-10-16 — matches the `/api/library` item shape; `progress_pct` stays library-list-only).
- `wayfinding.parts` (and the same object at `copies[].location.parts`) is the **structured** form of
  the ladder (additive; always present wherever `location`/`wayfinding` is non-null, all ten keys
  populated). The web composes localized wayfinding text from it
  ([14 §6.1](14-localization.md#61-wayfinding-vocabulary)); `ladder`/`ladder_steps` stay English
  prose for admin screens, tool results and logs.
- `wayfinding` is `null` when no copy has a shelf; otherwise it describes the **home shelf**: the
  first available shelved copy (by barcode), else the first shelved copy. `copies[].location.ladder`
  uses the [05 §3](05-building-layout.md#3-location-path-tokens) template, built in the service layer.
- `rating_avg` / `rating_count` come from the [`book_ratings` view](02-data-model.md#62-book_ratings) (null when no ratings).
- `neighbors` = other available books sharing `shelf_id` or the section
  ("archival affinity carousel" — [12](./12-ui-design.md#screen-3--booksid-book-detail--spatial-wayfinding)).
- When zero available copies this endpoint returns **200** still (details remain
  useful), and `availability.state` = the mismatch state; the 409
  (`BOOK_NOT_AVAILABLE`) fires only on availability-affecting actions — concretely
  `POST /api/library` with `{"status":"reading"}` on a book with zero available
  copies — with the `{suggested_actions:["reserve","notify_when_available"]}` body
  from the [Error envelope](#1-conventions). The detail page itself stays 200 (see
  [§5.2](#52-get-apibooksid)).

### 5.3 POST /api/books/fetch-isbn

Barcode intake pipeline — [11-barcode-scanning.md](./11-barcode-scanning.md);
rate limit 10/1min per user.

```json
// request
{ "isbn": "9780441013593" }
// response 200 — normalized metadata from Open Library (details: docs/08 §fallback)
{
  "isbn13": "9780441013593",
  "title": "Dune",
  "authors": ["Frank Herbert"],
  "publisher": "Ace Books",
  "published_year": 1965,
  "description": "…",
  "cover_url": "https://covers.openlibrary.org/b/isbn/9780441013593-L.jpg",
  "dewey_num": "813.54",
  "loc_num": "PS3553.I57 D8 1965",
  "open_library_key": "/works/OL45864W",
  "source": "openlibrary",     // "openlibrary" or "offline_mock"
  "in_catalog": true,          // false — safe to create; true → client opens existing book
  "similar": [
    {
      "id": "6bb2ef05…",
      "title": "Project Hail Mary",
      "authors": ["Andy Weir"],
      "isbn13": "9780593135204",
      "copies": 3,
      "section": { "code": "SF", "name": "Science Fiction & Speculative", "color": "#3B82F6" }
    }
  ]
}
```

- Checksum validation with `github.com/osamingo/checkdigit` (EAN-13) — invalid →
  400 `INVALID_ISBN` (client-side pre-filter is best effort, [11 §error handling](./11-barcode-scanning.md#8-error-handling)).
- Existing book (primary ISBN match) → 409 `ISBN_CONFLICT` + `{"book_id":"…"}` so admin navigates.
- `similar` (array, possibly empty): books in the catalog whose normalized title equals the
  looked-up title and share at least one author — direct string comparison in v1 (no trigram
  threshold). When non-empty the intake UI shows a **"looks like this is already in the catalog"
  banner** with `data-testid="intake-similar"`, offering **Add as another edition**
  (`data-testid="intake-attach"`, calls `POST /api/admin/books/{id}/isbns` on the chosen book
  then adds copies) or **Create a separate book anyway** (`data-testid="intake-create-new"`).
  Section/shelf remain required; section pre-fills from the matched book. Staff Intake only
  (English — [14 §1](14-localization.md#1-scope)).
- Open Library unreachable → 200 with `source:"offline_mock"` (filled with nulls)
  **iff `OFFLINE_MODE=false`**? **No** — see [08-seed-data.md](08-seed-data.md#6-open-library-fallback):
  when the network fetch fails after 2 retries the endpoint **still returns the
  barcode-validated partial record**: `title:""`,`source:"offline_partial"`,
  `covers:[]`, `warnings:["upstream_unavailable"]` (never blocks intake).
- **Admin-only (👑)**: this is the *intake* path (it may create a book). Patron barcode
  scanning uses the local resolver [§5.4](#54-post-apibooksscan) for instant lookup; if that returns
  `match:"none"` for an ISBN-13, the patron UI calls [§5.5](#55-post-apibooksidentify-isbn) to identify
  the book via Open Library — the scanner never hits Open Library through this admin endpoint.

### 5.4 POST /api/books/scan

Patron barcode/ISBN lookup ([11-barcode-scanning.md](./11-barcode-scanning.md#9-endpoints--validation),
🔒 any signed-in user); rate limit 30/1min per user. Resolves a scanned code **locally** — no
upstream call: an ISBN-13 looks up `books` (by `isbn13`), a copy tag looks up `book_copies`.

```json
// request
{ "code": "9780441013593" }          // ISBN-13/EAN-13 or an Athenaeum copy tag ATH-002941-C01
// response 200
{
  "code": "9780441013593",
  "code_type": "isbn13",             // "isbn13" | "copy_barcode"
  "in_catalog": true,
  "match": "book",                   // "book" | "copy" | "none"
  "book": {
    "id": "8c14…", "title": "Dune", "authors": ["Frank Herbert"],
    "isbn13": "9780441013593", "cover_url": "…",
    "section": { "code": "SF", "name": "Science Fiction & Speculative", "color": "#3B82F6" }
  },
  "copy": null,                      // populated only for a copy-tag match
  "availability": { "total_copies": 3, "available_copies": 2, "state": "available" },
  "wayfinding": { "path": "MAIN/3F/SF/A-1/S-2", "parts": { … }, "ladder_steps": [ … ] },
  "returnable": false,               // copy-tag: status "checked_out"; ISBN: caller has open loan
  "borrowable": false,               // copy-tag: status "available" + under limit; ISBN: ≥1 available copy or caller's own ready hold
  "loan": null,                      // copy-tag match: the copy's open loan (mine + due_at); null otherwise
  "borrow_copy": null,               // ISBN match only: {id, barcode, location:{path,parts}} — the copy that would be loaned
  "my_loans": []                     // ISBN match only: [{id, copy_id, barcode, due_at}] — caller's open loans for this book
}
```

- `match:"copy"` (copy tag): `copy` = `{id,barcode,status,condition,location:{path,parts,ladder_steps}}`;
  `book` is the copy's book; `wayfinding` is that copy's location. The client offers:
  - `returnable` = `copy.status == "checked_out"` → [§6.4 return](#64-post-apicopiesidreturn);
  - `borrowable` = `copy.status == "available"` **and** the caller has fewer than the
    [borrow limit](#65-post-apicopiesidborrow) → [§6.5 borrow](#65-post-apicopiesidborrow);
  - `loan` = `{id,user_id,due_at,renewals,mine}` for the copy's open loan, else `null`
    (`mine` = the caller is the borrower; `due_at` drives the "due in N days" copy).
- `match:"book"` (ISBN): `copy:null`; `wayfinding`/`availability` follow the
  home-shelf + availability rules of [§5.2](#52-get-apibooksid), so the client reads *other copies
  left* and offers the waitlist CTA when `available_copies == 0`.
  Additionally: `borrowable` = `available_copies > 0` **or** the caller has a `reserved_waitlist`
  copy ready for pickup; `borrow_copy` = the copy the server would pick (caller's own
  `reserved_waitlist` copy first, else the lowest available barcode) — `null` when not borrowable;
  `returnable` = the caller has at least one open loan for this book; `my_loans` = the caller's
  open loans for this book (empty array when none). These fields power the scanner's
  **Borrow this book** / **Return this book** actions ([§6.8](#68-post-apibooksidborrow) /
  [§6.9](#69-post-apibooksidreturn)).
- `in_catalog:false`, `match:"none"`, `book:null` when the code validates but no local row matches
  (patron cannot create a book; staff use [§5.3](#53-post-apibooksfetch-isbn)).
- Normalization: strip spaces/hyphens; `ATH-…` uppercased and matched against
  `^ATH-[0-9]{6}-C[0-9]{2}$`; an EAN-13 payload is re-validated with `checkdigit` → 400
  `INVALID_ISBN` on failure. The full `copies[]`/`neighbors` are **not** returned — the client
  deep-links to [§5.2](#52-get-apibooksid) for detail.

### 5.5 POST /api/books/identify-isbn

Patron **ISBN identification via Open Library** (🔒 any signed-in user); rate limit 10/1min per
user (bucket `identify_isbn`). Called by the scanner **only after** `POST /api/books/scan`
([§5.4](#54-post-apibooksscan)) returns `match:"none"` + `code_type:"isbn13"`. The "not in
library" card renders instantly from §5.4; then the UI calls this endpoint to fill in the OL
metadata. Traffic is throttled independently of the scan bucket ([11](./11-barcode-scanning.md#2-scan-flows)).

```json
// request
{ "isbn": "9780593355275" }
// response 200 — never 409, never 5xx for upstream trouble
{
  "isbn13": "9780593355275",
  "in_catalog": false,
  "book_id": null,
  "identified": {
    "title": "Project Hail Mary",
    "authors": ["Andy Weir"],
    "publisher": "Ballantine Books",
    "published_year": 2021,
    "cover_url": "https://covers.openlibrary.org/b/isbn/9780593355275-L.jpg"
  },
  "source": "openlibrary",
  "warnings": []
}
```

- `source` is one of:
  - `"openlibrary"` — Open Library returned metadata; `identified` is populated.
  - `"local"` — the ISBN is already in the catalog (`in_catalog:true`, `book_id` set, `identified:null`);
    no upstream call is made; the client should offer "View Full Record" instead of identification.
  - `"offline_partial"` — OL was not reachable or had no record; `identified:null`,
    `warnings` contains `"not_found"` (OL found but no data) or `"upstream_unavailable"` (OL down /
    `OFFLINE_MODE=true`).
- `identified` fields: `title` (string), `authors` (string array), `publisher` (string or null),
  `published_year` (int or null), `cover_url` (string — `https://covers.openlibrary.org/b/isbn/{isbn13}-L.jpg`
  when OL has a cover, else null). Classification fields (`dewey_num`, `loc_num`, `description`,
  `open_library_key`) are omitted — patron-facing only; admin intake uses [§5.3](#53-post-apibooksfetch-isbn).
- Checksum validation with `pkg/checkdigit` EAN-13 — invalid → 400 `INVALID_ISBN`.
- Uses the **same Redis ISBN cache** (24 h, [08-seed-data.md](./08-seed-data.md#6-open-library-fallback)) and
  `olbooks` client (3 s / 2 retries) as `fetch-isbn` and `cmd/seed`.
- The ISBN may have been cataloged between the §5.4 scan and this call — the service re-checks
  `books` for a concurrent add; if found, returns `in_catalog:true` + `source:"local"`.

| Scenario | `in_catalog` | `book_id` | `identified` | `source` | `warnings` |
| --- | --- | --- | --- | --- | --- |
| ISBN in catalog | true | set | null | local | [] |
| OL found | false | null | populated | openlibrary | [] |
| OL has no record | false | null | null | offline_partial | ["not_found"] |
| OL down / `OFFLINE_MODE` | false | null | null | offline_partial | ["upstream_unavailable"] |
| Bad checksum | — | — | — | — | 400 `INVALID_ISBN` |

### 5.6 GET /api/books/{id}/cover

Serves the book's cover image bytes from `book_covers`
([02](02-data-model.md#book_covers), [08 §6a](08-seed-data.md#6a-cover-cache)). Public
(🔓): covers are not sensitive and `<img>` loads must not depend on the access-token
refresh flow. Not rate limited ([§2](#2-rate-limiting)).

**`cover_url` in every book payload** (search items, detail, neighbors, library, loans,
waitlist, chat citations, scan results) is this route — `/api/books/{id}/cover`, with
`?v={12 hex chars}` (sha256 prefix of the cached bytes) once cached — or `null` when the
book has no cover. Browsers resolve it against the web origin, which proxies `/api/*`.
The admin book schema ([§8.2](#82-admin-books)) is the exception: its `cover_url` is the
raw upstream URL stored in `books.cover_url`, which is what `PATCH` edits.
`fetch-isbn` / `identify-isbn` metadata also return upstream URLs (no book row yet).

- **Hit** → `200`, body = image bytes, `Content-Type` as stored,
  `ETag: "{sha256}"`, `Cache-Control: public, max-age=31536000, immutable`
  (the `?v=` query changes whenever the bytes do). `If-None-Match` matching → `304`.
- **Miss** → read-through: the API fetches `books.cover_url` once (10 s timeout,
  concurrent requests for one URL share a single fetch), stores it, and serves it as
  above. Skipped when `OFFLINE_MODE=true`.
- `404 NOT_FOUND` when the book is unknown (or `{id}` is not a UUID), has no
  `cover_url`, upstream has no usable image (404, non-image, < 1 KiB placeholder,
  > 2 MiB), or the fetch failed / was skipped offline. A failed URL is not retried for
  15 minutes. The web `BookCover` component renders its typographic placeholder on 404.

## 6. Personal Library, Waitlist & Loans

### 6.1 GET /api/library

Personal list. Params: `status=want_to_read|reading|read|dropped|favorites` (repeatable),
`include_waitlist=false` default (adds `hold` items with `waitlist` subresource).

```json
{ "items": [
  {
    "book": { "id": "…", "title": "Dune", "cover_url": "…", "authors": ["Frank Herbert"] },
    "location": { "floor": 3, "shelf_code": "S-2", "path": "MAIN/3F/SF/A-1/S-2" },
    "status": "reading",
    "is_favorite": false,
    "progress_pct": 40,
    "rating": null,
    "added_at": "2026-10-01T10:00:00Z"
  }
], "meta": { "page": 1, "per_page": 20, "total": 24, "total_pages": 2 } }
```

Additive fields: every item carries `"kind":"entry"`; `include_waitlist=true` prepends
`{"kind":"hold","book":{…},"waitlist":{"id","status","position",…}}` items on page 1 (not counted in
`meta.total`); and the response carries `"counts":{"total","reading","read","want_to_read","dropped","favorites"}`
for the KPI cards regardless of the active filter.

### 6.2 Add/Update library entry

`POST /api/library` body `{ "book_id": "…", "status": "reading?" }` → 201;
`PATCH /api/library/{book_id}` body any of
`{ "status": "want_to_read|reading|read|dropped", "is_favorite": true|false, "progress_pct": 40, "rating": 5 }`
→ 200 upserted row; `DELETE /api/library/{book_id}` → 204.

- Upsert semantics (idempotent): `POST` returns 201 for a new row and 200 when it already existed;
  `PATCH` on a missing row creates it as `want_to_read`; `null` clears `progress_pct`/`rating`. 404 for unknown book.
- Zero-available copy + first-time `status=reading` → **409 BOOK_NOT_AVAILABLE**
  (treated as soft warning; client may proceed with `?force=true` — captures
  suggested_actions from the 409 body).

### 6.3 Waitlist

| Call | Body | Response |
| --- | --- | --- |
| `POST /api/books/{id}/waitlist` | — | `201 {"id":"w1…","status":"waiting","position":2}` |
| `DELETE /api/books/{id}/waitlist` | — | 204 (sets cancelled) |
| `GET /api/waitlist` | — | `{ "items":[ {"id":"w1…","book":{…},"status":"waiting","position":1} ] }` |

- Only meaningful when `availability.state` has zero available copies (otherwise 409 `CONFLICT`);
  joining twice → same row (200 with existing), no duplicate ([02](02-data-model.md#waitlist)).
- Expiry sweep (every 5 min in the API process): `ready` rows past `expires_at` (ready_at + 48 h)
  become `expired`; the freed hold goes to the oldest `waiting` patron (→ `ready`, new 48 h window), or,
  with nobody waiting, one `reserved_waitlist` copy returns to `available` (`copy_events.source='waitlist'`).
  Cancelling a `ready` hold does the same. Marking copies `reserved_waitlist` on turnover happens
  via the admin copy PATCH **or** a patron self-service return ([§6.4](#64-post-apicopiesidreturn)).
- `notify_when_available` UX is in-app only — no email delivery ([10-roadmap.md](./10-roadmap.md)).

### 6.4 POST /api/copies/{id}/return

Patron **self-service return** (🔒 any signed-in user — [11](./11-barcode-scanning.md#2-scan-flows));
rate limit 10/1min per user. Trust-based desk return: any patron scanning a `checked_out`
copy's tag may return it. Body is empty. The open loan (if any) is closed
(`returned_at = now()` — [02 §loans](02-data-model.md#loans)).

```json
// response 200
{
  "copy": { "id": "b1e3…", "barcode": "ATH-002941-C01", "status": "available", "condition": "good",
            "last_state_change": "2026-10-15T09:12:00Z",
            "location": { "path": "MAIN/3F/SF/A-1/S-2", "parts": { … }, "ladder_steps": [ … ] } },
  "availability": { "total_copies": 3, "available_copies": 3, "state": "available" },
  "hold": null,
  "closed_loan": { "id": "…", "returned_at": "2026-10-15T09:12:00Z" }   // null when the copy had no open loan
}
```

- Effect: `book_copies.status` `checked_out → available` (re-shelved at its existing `shelf_id`),
  the copy's open loan (if any) gets `returned_at = now()`, `last_state_change = now()`,
  and a `copy_events` row is appended
  (`from_status:"checked_out"`, `to_status:"available"`, `source:"self_return"`,
  `actor_id` = caller — [02 §copy_events](02-data-model.md#copy_events)).
- **Waitlist turnover:** when the book has `waiting` holds, the returned copy becomes
  `reserved_waitlist` and the oldest `waiting` hold goes `ready` (`ready_at = now()`,
  `expires_at = +48h`) — the same turnover the [§6.3](#63-waitlist) sweep performs. The `hold`
  field then names the promoted hold (`{"id","status":"ready","expires_at"}`), else `null`.
- Errors: unknown copy → 404 `NOT_FOUND`; `status != "checked_out"` → 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"Copy is not checked out","current_status":"available"}`).

### 6.5 POST /api/copies/{id}/borrow

Patron **self-service checkout** (🔒 any signed-in user — [11](./11-barcode-scanning.md#2-scan-flows));
rate limit 10/1min per user. Scanning a copy tag starts a loan. Body is empty. This is the
counterpart to [§6.4](#64-post-apicopiesidreturn) and supersedes the earlier "return-only"
scope ([10 §1](10-roadmap.md#1-in-scope-v1--checklist) item 17).

```json
// response 201
{
  "loan": { "id": "…", "copy_id": "b1e3…", "user_id": "…", "borrowed_at": "2026-10-16T09:00:00Z",
            "due_at": "2026-10-30T09:00:00Z", "returned_at": null, "renewals": 0, "renewable": true },
  "copy": { "id": "b1e3…", "barcode": "ATH-002941-C01", "status": "checked_out", "condition": "good",
            "last_state_change": "2026-10-16T09:00:00Z",
            "location": { "path": "MAIN/3F/SF/A-1/S-2", "parts": { … }, "ladder_steps": [ … ] } },
  "availability": { "total_copies": 3, "available_copies": 1, "state": "available" }
}
```

- Effect (one transaction): `book_copies.status` → `checked_out`, `last_state_change = now()`,
  a `loans` row inserted (`borrowed_at = now()`, `due_at = now() + LOAN_PERIOD`, `renewals = 0`),
  and a `copy_events` row appended (`source:"self_checkout"`, `actor_id` = caller).
- Rules: `status` must be `available` for any patron, **or** `reserved_waitlist` **owned by the
  caller** (a ready hold being picked up — the hold goes `picked_up`). `LOAN_PERIOD = 14 days`,
  max **5** open loans per user.
- Errors: unknown copy → 404 `NOT_FOUND`; `status` is `checked_out`/`missing_lost`, or
  `reserved_waitlist` held by someone else → 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"Copy is not available to borrow","current_status":"checked_out"}`);
  borrower at the 5-loan limit → 409 `BORROW_LIMIT`
  (`{"error":"BORROW_LIMIT","message":"You already have 5 loans","limit":5}`).

### 6.6 POST /api/copies/{id}/renew

Extend an open loan by `LOAN_PERIOD` on the borrower's own loan (🔒); rate limit 5/1min per
user. Body is empty.

```json
// response 200
{ "loan": { "id": "…", "due_at": "2026-11-13T09:00:00Z", "renewals": 1, "renewable": false } }
```

- Rules: only the borrower may renew their own loan; `renewals` may go to at most **1**
  (`due_at += 14d`, `renewals = 1`). `renewable` is `false` once renewed (or when the loan
  is returned).
- Errors: unknown copy → 404 `NOT_FOUND`; no open loan → 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"No open loan on this copy"}`); not the borrower → 403
  `FORBIDDEN`; already renewed → 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"Loan already renewed","renewals":1}`).

### 6.7 GET /api/loans

The caller's loans (🔒). Active loans first (soonest `due_at`), then history (newest
`returned_at`). Drives the `/library` **Loans** tab ([07](07-frontend.md#library--screen-4-stitch-9cee3484)).

```json
{ "items": [
  { "kind": "active", "id": "…", "book": { "id": "…", "title": "Dune", "cover_url": "…", "authors": ["Frank Herbert"] },
    "copy": { "id": "b1e3…", "barcode": "ATH-002941-C01" },
    "borrowed_at": "2026-10-16T09:00:00Z", "due_at": "2026-10-30T09:00:00Z",
    "renewals": 0, "renewable": true, "overdue": false },
  { "kind": "past", "id": "…", "book": { … }, "copy": { … },
    "borrowed_at": "…", "due_at": "…", "returned_at": "…", "renewals": 1, "renewable": false, "overdue": false }
], "meta": { "active": 2, "overdue": 0 } }
```

- `overdue` = `now() > due_at` on an open loan; `renewable` mirrors [§6.6](#66-post-apicopiesidrenew).
  No fines and no email reminders (deferred — [10 §2](10-roadmap.md#2-out-of-scope--deferred)).

### 6.8 POST /api/books/{id}/borrow

Patron **borrow by book** (🔒 any signed-in user); the server picks which copy to loan.
Rate limit 10/1min per user (shared bucket with `POST /api/copies/{id}/borrow`). Body is
empty. The copy is selected as: caller's own `reserved_waitlist` copy first (ready hold
being picked up), else the copy with the lowest available barcode (`FOR UPDATE SKIP LOCKED`
to handle concurrency).

```json
// response 201
{
  "loan": { "id": "…", "copy_id": "b1e3…", "user_id": "…", "borrowed_at": "2026-10-16T09:00:00Z",
            "due_at": "2026-10-30T09:00:00Z", "returned_at": null, "renewals": 0, "renewable": true },
  "copy": { "id": "b1e3…", "barcode": "ATH-002948-C02", "status": "checked_out", "condition": "good",
            "location": { "path": "MAIN/3F/SF/A-3/S-1", "parts": { … }, "ladder_steps": [ … ] } },
  "availability": { "total_copies": 3, "available_copies": 2, "state": "available" }
}
```

- Same rules as [§6.5](#65-post-apicopiesidborrow): `LOAN_PERIOD = 14d`, max 5 open loans
  per user. The scanner UI tells the patron **which copy tag** to take and **which shelf** it is on.
  Test id `scan-borrow` on the action button; `scan-borrow-copy` on the chosen barcode + shelf display.
- Errors: 404 `NOT_FOUND` (unknown book); 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"No available copy to borrow"}`) when all copies are
  checked out / `reserved_waitlist` by others; 409 `BORROW_LIMIT` when the caller already
  has 5 loans.

### 6.9 POST /api/books/{id}/return

Patron **return by book** (🔒); closes the caller's **oldest open loan** for the specified
book. Rate limit 10/1min per user (shared bucket with `POST /api/copies/{id}/return`). Body
is empty.

```json
// response 200
{
  "loan": { "id": "…", "copy_id": "b1e3…", "returned_at": "2026-10-16T14:00:00Z" },
  "copy": { "id": "b1e3…", "barcode": "ATH-002948-C02", "status": "available", "condition": "good",
            "location": { "path": "MAIN/3F/SF/A-3/S-1", "parts": { … }, "ladder_steps": [ … ] } },
  "availability": { "total_copies": 3, "available_copies": 3, "state": "available" },
  "hold": null,
  "closed_loan": { "id": "…", "returned_at": "2026-10-16T14:00:00Z" }
}
```

- Effect mirrors [§6.4](#64-post-apicopiesidreturn): `checked_out → available` (or
  `reserved_waitlist` on hold turnover), open loan closed (`returned_at = now()`), `copy_events`
  appended (`source:"self_return"`). Test id `scan-return` on the action button.
- Errors: 404 `NOT_FOUND` (unknown book); 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"No open loan found for this book"}`) when the caller has no
  open loan for the book.

## 7. Chat (SSE)

Non-streaming conversation CRUD is plain JSON, all for the calling user only:

| Endpoint | Notes |
| --- | --- |
| `GET /api/chat/conversations` | `{items:[{id,title,updated_at}], meta}` |
| `POST /api/chat/conversations` | body `{title?}` → 201 |
| `GET /api/chat/conversations/{id}` | `{id,title,messages:[{id,role,content,tool_meta,created_at}]}`; assistant messages add `citations:[{book_id,title,path,snippet,score,cover_url}]` (persisted `citations` event, [06 §7](06-ai-rag.md#7-conversation-persistence); `[]` when none, `tool_meta` null) plus `thinking` (persisted `thinking` events, string) and `thinking_ms` (int) — both `null` when the model streamed no thinking |
| `DELETE /api/chat/conversations/{id}` | 204 |

### 7.1 SSE stream

Detailed event pipeline, RAG retrieval, and tool-use are specified in
[06-ai-rag.md](./06-ai-rag.md). This section is the HTTP shell only.

`POST /api/chat` (always `Accept: text/event-stream`):

```json
{ "conversation_id": "c0f1…", "message": "Where can I find books on Babylonian mathematics?", "locale": "en" }
```

`locale` is optional (`"en"` | `"zh-CN"`, default `en`): it selects the AI reply
language and is echoed back in the `meta` event — contract in
[14 §10](./14-localization.md#10-ai-reply-language). Unknown values fall back to `en`.

Lifecycle: rate-limit checks (5/min + 50/day, [§2](#2-rate-limiting)) → create
conversation when `conversation_id` absent → persist user message → RAG retrieval
(pgvector Top-K) → Eino agent loop with tools ([06 §tools](06-ai-rag.md#4-tool-definitions)) →
stream reply tokens → persist assistant + tool messages → `done`.

Headers on the 200: `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
`Connection: keep-alive`. Errors before first byte = regular JSON envelope + status
code; mid-stream errors emit `event: error` before `done` (no further events after).

### 7.2 `POST /api/chat/transcribe`

Voice input for `/chat` (🔒, full contract: [15-voice-input.md](./15-voice-input.md)).
`multipart/form-data` with an `audio` file part and an optional `locale` form field
(`en` | `zh-CN` → Whisper `language` hint). Optional query parameter `partial`
(`true` | `false`, default `false`): `?partial=true` requests a **live (interim)** pass over
the cumulative clip recorded so far ([15 §2a](./15-voice-input.md#2a-live-interim-transcript)) — same body,
response shape and caps, served by the smaller/faster Whisper (`WHISPER_PARTIAL_BASE_URL`)
under a 15 s upstream timeout. The flag is a query parameter (not a form field) so the
rate limiter can pick the bucket before the body is read.

```json
{ "text": "where can I find books on Babylonian mathematics", "language": "en" }
```

Audio is never persisted or logged; the transcript is returned as text and only becomes
a chat turn when the client sends it via [§7.1](#71-sse-stream). Rate limit: 10/min
per `user_id` for final calls; partials use a separate 60/min bucket and at most one
partial may be in flight per user ([§2](#2-rate-limiting)). Caps: 60 s / ~10 MB; 30 s
upstream timeout (15 s for partials).

| Error | HTTP | When |
| --- | --- | --- |
| `VALIDATION_FAILED` | 400 | missing/undecodable audio, over caps, `partial` not a boolean |
| `RATE_LIMITED` | 429 | over the 10/min bucket (final) or the 60/min bucket (partial); a second concurrent partial for the same user (`Retry-After: 1`) |
| `TRANSCRIPTION_UNAVAILABLE` | 503 | Whisper service down or timeout |
| — | 200 | silent/unintelligible clip → `{"text":"","language":"en"}` |

## 8. Admin

All admin endpoints require `role=admin` ([04-auth.md](./04-auth.md#7-roles--route-protection));
non-admin → 403 `FORBIDDEN`. CUD mutations append `copy_events`
([02](02-data-model.md#copy_events)) for the mirrored audit stream.

### 8.1 Admin KPIs & audit

`GET /api/admin/data` (KPI cards of [07 §5.4](07-frontend.md#admin--screen-6a-stitch-3177da52)):

```json
{
  "titles_in_catalog": 40,
  "copies": { "total": 60, "available": 48, "checked_out": 12, "missing_lost": 0 },
  "shelf_capacity": { "occupied": 49, "total": 82 },
  "active_patrons": 24,
  "chat_week": { "conversations": 19, "messages": 132 },
  "computed_at": "2026-10-06T21:14:52Z"
}
```

`GET /api/admin/copy-events?since=<RFC3339>&limit=100` — the "real-time audit
stream" (client-server polling every 3 s; WebSockets out of scope
[10](./10-roadmap.md)):

```json
{ "items": [
  {
    "id": 771,
    "barcode": "ATH-002941-C02",
    "book_title": "Dune",
    "from_status": "checked_out",
    "to_status": "available",
    "source": "admin_patch",
    "actor": { "id": "…", "name": "Archivist Q" },
    "created_at": "2026-10-06T19:52:03Z"
  }
] }
```

`GET /api/admin/index-status` — auto-index backlog ([06 §2](06-ai-rag.md#2-rag-pipeline)).
Pending is exactly the worker's set: books with `embedding_dirty = true`, plus books
with a non-blank `description` and zero `book_embeddings` rows
([02](02-data-model.md#book_embeddings)). `pending_count` is the full count;
`pending_book_ids` lists at most 200 ids in worker order (dirty first, then oldest
`updated_at`) and is `[]` when the backlog is empty. General 120/min bucket
([§2](#2-rate-limiting)).

```json
{ "pending_count": 2, "pending_book_ids": ["8c14…", "1f0a…"] }
```

### 8.2 Admin books

`GET /api/admin/books?search=&shelf=&page=&per_page=&sort=updated_at:desc` returns a
list per [§1 pagination](#1-conventions). An empty search browses the list; `search`
matching covers title, authors, the primary ISBN and every edition ISBN. Each item
includes `updated_at`, integer `active_loans` (the count of open loans, where
`returned_at IS NULL`) and boolean `removable`. `removable` is false if any copy
is `checked_out` or `reserved_waitlist`, or if any open loan exists. A book can have
`active_loans: 0` and still be blocked.

- `POST /api/admin/books` body `{book fields}+"copies":[{"barcode?","status?","shelf_path?":…}]`
  → 201 full book (barcodes auto-generated `ATH-XXXXXX-CY` server-side when omitted —
  available/reserve paths the accession counter; [13](08-seed-data.md#5-barcode-allocation)).
- `PATCH /api/admin/books/{id}` (partial; e.g. cover_url, section, description).
- `DELETE /api/admin/books/{id}` → 204 when removable, 404 if missing, or 409
  `CONFLICT` if any copy is `checked_out` or `reserved_waitlist`, or has an open loan.
- `POST /api/admin/books/bulk-delete` body `{ "ids": ["<uuid>", "<uuid>"] }`
  → 204. The list must contain 1–50 distinct valid UUIDs; invalid input or duplicate
  IDs → 400 `VALIDATION_FAILED`, a missing book → 404 `NOT_FOUND`, and any blocked
  book → 409 `CONFLICT`. The operation is atomic: any failure leaves every requested
  book intact.

Both deletion endpoints return the blocked books in the 409 error envelope:

```json
{
  "error": "CONFLICT",
  "message": "Books have active loans, checked-out copies, or reserved copies.",
  "blocked": [{ "id": "<uuid>", "title": "Dune", "active_loans": 0 }]
}
```

Successful book removal cascades copies, their copy events and past loan history,
edition ISBNs, embeddings, personal-library and waitlist rows
([02 FKs](02-data-model.md#4-table-ddl-canonical-sql)). A `missing_lost` copy alone
does not block removal; use merge to consolidate duplicates.

### 8.3 Admin copies

- `POST /api/admin/copies` body `{"book_id":"…","barcode?":"ATH-002941-C04","shelf_path":"MAIN/3F/SF/A-1/S-2","status":"available","condition":"fair"}`
  → 201 row; unknown shelf_path → 404 `NOT_FOUND` (resolution algorithm: [05](./05-building-layout.md#4-wayfinding-resolution)).
- `PATCH /api/admin/copies/{id}` body `{"status":"checked_out","shelf_id":"…","condition":"missing"}`
  → 200; appends `copy_events` row (`from_status ≠ to_status` recorded). Barcode is
  immutable once registered ([13](08-seed-data.md#5-barcode-allocation)).
- `DELETE /api/admin/copies/{id}` → 204 (hard delete; `copy_events` rows cascade-delete with the copy — decided 2026-10-16, matches migration 0002 `ON DELETE CASCADE`; the admin audit stream shows only surviving copies' events).

Barcode state machine semantics: [12-ui-design.md](./12-ui-design.md#10-copy-state-machine).

### 8.4 Admin tree & building

- `GET /api/admin/tree` → one building + nested `floors[].sections[].aisles[].shelves[]`
  with per-shelf `{id, code, occupied, total}` aggregates for the
  `BuildingTreeBrowser` ([07](07-frontend.md#admin--screen-6a-stitch-3177da52), [12](12-ui-design.md#screen-6--admin-and-adminlayout-staff-admin--stacks-manager)).
- `GET /api/admin/shelves/{id}` → shelf row + copy slots manifest + adjacent shelves.

```json
{
  "id": "6f19…",
  "code": "S-2",
  "aisle": "A-1",
  "section": { "code": "SF", "name": "Science Fiction", "color": "#3B82F6" },
  "row_level": 4,
  "height_tier": "Eye Level",
  "occupancy": { "occupied": 38, "total": 45 },
  "copies": [
    { "id": "b1e3…", "barcode": "ATH-002941-C01", "status": "available", "book_id": "8c14…", "title": "Dune" }
  ]
}
```

### 8.5 Admin users

`GET /api/admin/users?page=` (paginated users + joined stats:
library item counts, waitlist counts, last login from `refresh_tokens.created_at`).
`PATCH /api/admin/users/{id}` body `{"role":"admin"|"user"}` → 200 user; can't
demote yourself (`409 FORBIDDEN`? — client-guarded, server 400 `VALIDATION_FAILED`).

### 8.6 Admin ISBN management & merge

#### POST /api/admin/books/{id}/isbns

Attach an additional edition ISBN to an existing book. Body: `{ "isbn13": "9780593355275" }`.
Response **201**:

```json
{ "book_id": "6bb2ef05…", "isbns": [{ "isbn13": "9780593135204", "is_primary": true }, { "isbn13": "9780593355275", "is_primary": false }] }
```

- Checksum-validated. If the ISBN already belongs to **another** book → 409 `ISBN_CONFLICT`
  + `{"book_id":"…"}`. If the ISBN is already attached to this book → 200 (idempotent). The
  first ISBN attached this way becomes a non-primary alias; `books.isbn13` (the primary) is
  unchanged. Test id `book-merge` on the admin merge action button.

#### DELETE /api/admin/books/{id}/isbns/{isbn13}

Detach a non-primary edition ISBN. Response **204**.

- Attempting to delete the primary ISBN → 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"Cannot remove the primary ISBN"}`). To change the primary,
  first attach a replacement and update `books.isbn13` via `PATCH /api/admin/books/{id}`.

#### POST /api/admin/books/{id}/merge

Merge the **source** book (`{id}` in the path) into the **target** book. Body:
`{ "into": "<target_book_id>" }`. Response **200**:

```json
{ "book_id": "<target_id>", "merged_copies": 3, "isbns": [{ "isbn13": "9780593135204", "is_primary": true }, { "isbn13": "9780593355275", "is_primary": false }] }
```

One transaction — full semantics in [02 §book_isbns merge](02-data-model.md#admin-merge-post-apiadminbooksidmerge).
Test id `merge-confirm` on the confirmation dialog confirm button. Errors: 404 for unknown
source or target; 400 `VALIDATION_FAILED` if source == target.

## 9. Error Catalog

| Code | HTTP | Meaning | Raised by |
| --- | --- | --- | --- |
| `VALIDATION_FAILED` | 400 | generic body/param validation | all request binders |
| `INVALID_ISBN` | 400 | EAN-13 checksum failure | `fetch-isbn`, `books/scan`, [11](./11-barcode-scanning.md) |
| `UNAUTHENTICATED` | 401 | no/invalid access token | auth middleware |
| `REUSED_TOKEN` | 401 | refresh token replay — session killed | `/api/auth/refresh` |
| `INVALID_CREDENTIALS` | 401 | login mismatch (exact same text) | login |
| `FORBIDDEN` | 403 | non-admin on admin route / foreign resource | middleware, ownership checks |
| `NOT_FOUND` | 404 | unknown id/path resolution | all `…/{id}` routes |
| `CONFLICT` | 409 | generic uniqueness / invalid state transition | admin create/update, self-service return |
| `EMAIL_TAKEN` | 409 | register duplicate | register |
| `ISBN_CONFLICT` | 409 | fetch-isbn existing | `fetch-isbn` |
| `BOOK_NOT_AVAILABLE` | 409 | availability rules above | detail/library actions |
| `RATE_LIMITED` | 429 | [§2](#2-rate-limiting) tracks | middleware |
| `UPSTREAM_ERROR` | 502 | OpenRouter/OpenLibrary outage | chat, fetch-isbn |
| `EMBEDDING_UNAVAILABLE` | 503 | Ollama down | semantic search, chat RAG |
| `TRANSCRIPTION_UNAVAILABLE` | 503 | Whisper down or timeout | `/api/chat/transcribe` ([15](./15-voice-input.md#6-limits-rate--errors)) |
| `UNEXPECTED` | 500 | catchall | handler top |

## 10. Traceability

| Domain | Doc owning definition |
| --- | --- |
| Tables/columns in response payloads | [02-data-model.md](./02-data-model.md) |
| JWT claims, cookie names, SSO flows | [04-auth.md](./04-auth.md) |
| Building path/ladder fields | [05-building-layout.md](./05-building-layout.md) |
| RAG retrieval params (Top-K, chunking) | [06-ai-rag.md](./06-ai-rag.md) |
| Page-by-page usage | [07-frontend.md](./07-frontend.md) |
| Barcode scanner (patron lookup + self-service return + staff intake) | [11-barcode-scanning.md](./11-barcode-scanning.md) |
| Component expectations per payload | [12-ui-design.md](./12-ui-design.md) |
| Seeded fixture values | [13-mock-data-reference.md](./13-mock-data-reference.md) |
