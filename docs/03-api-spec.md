# API Specification

> Canonical HTTP contract for the Athenaeum Go API: every endpoint with request/response JSON, error envelope, pagination, filtering, rate limits, and the SSE chat stream. Handlers in [01-architecture.md](./01-architecture.md), tables in [02-data-model.md](./02-data-model.md).

## Contents

1. [Conventions](#1-conventions)
2. [Rate Limiting](#2-rate-limiting)
3. [Endpoint Index](#3-endpoint-index)
4. [Auth Endpoints](#4-auth-endpoints)
5. [Books & Search](#5-books--search)
6. [Personal Library & Waitlist](#6-personal-library--waitlist)
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
| `q` | free text (required unless `mode=keyword` + empty) | supports `""` phrase syntax |
| `mode` | `keyword` \| `semantic` \| `hybrid` (default `keyword`) | hybrid = RRF k=60 |
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
      "score": 0.874
    }
  ],
  "meta": { "page": 1, "per_page": 20, "total": 40, "total_pages": 2 }
}
```

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
  "in_catalog": true           // false — safe to create; true → client opens existing book
}
```

- Checksum validation with `github.com/osamingo/checkdigit` (EAN-13) — invalid →
  400 `INVALID_ISBN` (client-side pre-filter is best effort, [11 §error handling](./11-barcode-scanning.md#8-error-handling)).
- Existing book → 409 `ISBN_CONFLICT` + `{"book_id":"…"}` so admin navigates.
- Open Library unreachable → 200 with `source:"offline_mock"` (filled with nulls)
  **iff `OFFLINE_MODE=false`**? **No** — see [08-seed-data.md](08-seed-data.md#6-open-library-fallback):
  when the network fetch fails after 2 retries the endpoint **still returns the
  barcode-validated partial record**: `title:""`,`source:"offline_partial"`,
  `covers:[]`, `warnings:["upstream_unavailable"]` (never blocks intake).
- **Admin-only (👑)**: this is the *intake* path (it may create a book). Patron barcode
  scanning uses the local resolver [§5.4](#54-post-apibooksscan) instead and never hits Open Library.

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
  "returnable": false                // true only for a copy-tag match whose status is "checked_out"
}
```

- `match:"copy"` (copy tag): `copy` = `{id,barcode,status,condition,location:{path,parts,ladder_steps}}`;
  `book` is the copy's book; `wayfinding` is that copy's location; `returnable` =
  `copy.status == "checked_out"` → the client may call [§6.4](#64-post-apicopiesidreturn).
- `match:"book"` (ISBN): `copy:null`, `returnable:false`; `wayfinding`/`availability` follow the
  home-shelf + availability rules of [§5.2](#52-get-apibooksid), so the client reads *other copies
  left* and offers the waitlist CTA when `available_copies == 0`.
- `in_catalog:false`, `match:"none"`, `book:null` when the code validates but no local row matches
  (patron cannot create a book; staff use [§5.3](#53-post-apibooksfetch-isbn)).
- Normalization: strip spaces/hyphens; `ATH-…` uppercased and matched against
  `^ATH-[0-9]{6}-C[0-9]{2}$`; an EAN-13 payload is re-validated with `checkdigit` → 400
  `INVALID_ISBN` on failure. The full `copies[]`/`neighbors` are **not** returned — the client
  deep-links to [§5.2](#52-get-apibooksid) for detail.

## 6. Personal Library & Waitlist

### 6.1 GET /api/library

Personal list. Params: `status=want_to_read|reading|read|favorites` (repeatable),
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
`meta.total`); and the response carries `"counts":{"total","reading","read","want_to_read","favorites"}`
for the KPI cards regardless of the active filter.

### 6.2 Add/Update library entry

`POST /api/library` body `{ "book_id": "…", "status": "reading?" }` → 201;
`PATCH /api/library/{book_id}` body any of
`{ "status": "want_to_read|reading|read", "is_favorite": true|false, "progress_pct": 40, "rating": 5 }`
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
rate limit 10/1min per user. This is return-only: no borrower link and no due dates exist
([01 §8](./01-architecture.md#8-cross-cutting-rules)), so any patron scanning a `checked_out`
copy's tag may return it (trust-based desk return). Body is empty.

```json
// response 200
{
  "copy": { "id": "b1e3…", "barcode": "ATH-002941-C01", "status": "available", "condition": "good",
            "last_state_change": "2026-10-15T09:12:00Z",
            "location": { "path": "MAIN/3F/SF/A-1/S-2", "parts": { … }, "ladder_steps": [ … ] } },
  "availability": { "total_copies": 3, "available_copies": 3, "state": "available" },
  "hold": null
}
```

- Effect: `book_copies.status` `checked_out → available` (re-shelved at its existing `shelf_id`),
  `last_state_change = now()`, and a `copy_events` row is appended
  (`from_status:"checked_out"`, `to_status:"available"`, `source:"self_return"`,
  `actor_id` = caller — [02 §copy_events](02-data-model.md#copy_events)).
- **Waitlist turnover:** when the book has `waiting` holds, the returned copy becomes
  `reserved_waitlist` and the oldest `waiting` hold goes `ready` (`ready_at = now()`,
  `expires_at = +48h`) — the same turnover the [§6.3](#63-waitlist) sweep performs. The `hold`
  field then names the promoted hold (`{"id","status":"ready","expires_at"}`), else `null`.
- Errors: unknown copy → 404 `NOT_FOUND`; `status != "checked_out"` → 409 `CONFLICT`
  (`{"error":"CONFLICT","message":"Copy is not checked out","current_status":"available"}`).

## 7. Chat (SSE)

Non-streaming conversation CRUD is plain JSON, all for the calling user only:

| Endpoint | Notes |
| --- | --- |
| `GET /api/chat/conversations` | `{items:[{id,title,updated_at}], meta}` |
| `POST /api/chat/conversations` | body `{title?}` → 201 |
| `GET /api/chat/conversations/{id}` | `{id,title,messages:[{id,role,content,tool_meta,created_at}]}`; assistant messages add `citations:[{book_id,title,path,snippet,score,cover_url}]` (persisted `citations` event, [06 §7](06-ai-rag.md#7-conversation-persistence); `[]` when none, `tool_meta` null) |
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

### 8.2 Admin books

`GET /api/admin/books?search=&shelf=&page=&per_page=&sort=updated_at:desc` (list per
[§1 pagination](#1-conventions), same item schema as detail minus `neighbors`,
plus `updated_at`).

- `POST /api/admin/books` body `{book fields}+"copies":[{"barcode?","status?","shelf_path?":…}]`
  → 201 full book (barcodes auto-generated `ATH-XXXXXX-CY` server-side when omitted —
  available/reserve paths the accession counter; [13](08-seed-data.md#5-barcode-allocation)).
- `PATCH /api/admin/books/{id}` (partial; e.g. cover_url, section, description).
- `DELETE /api/admin/books/{id}` — cascades copies/embeddings/user_library rows
  ([02 FKs](02-data-model.md#4-table-ddl-canonical-sql)); waitlist rows cascade too.

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
