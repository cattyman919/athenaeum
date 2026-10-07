# Seed Data & Mocks

> Seed architecture: `mocks/building.json` + `mocks/books.json` schemas, the deterministic seed algorithm, failure modes, reindex behavior, and Open Library enrichment/fallback — concrete fixture values (all 40 books, all shelves) in [13-mock-data-reference.md](./13-mock-data-reference.md), hierarchy semantics in [05-building-layout.md](./05-building-layout.md).

## Contents

1. [Files](#1-files)
2. [`mocks/building.json` Schema](#2-mocksbuildingjson-schema)
3. [`mocks/books.json` Schema](#3-mocksbooksjson-schema)
4. [Seed Algorithm](#4-seed-algorithm)
5. [Barcode Allocation](#5-barcode-allocation)
6. [Open Library Fallback](#6-open-library-fallback)
7. [Reindex](#7-reindex)
8. [Failure Modes](#8-failure-modes)
9. [Idempotency & Reset](#9-idempotency--reset)

## 1. Files

| File | Contents | Consumer |
| --- | --- | --- |
| `api/mocks/building.json` | 1 building, 3 floors, 5 sections, 15 aisles, 40 shelves | `cmd/seed` |
| `api/mocks/books.json` | 40 books, ~60 copies (12 `checked_out`), 5 zero-copy books, 2 demo users implied | `cmd/seed` |

Target distribution mirrors [13-mock-data-reference.md](./13-mock-data-reference.md):
60 copies → 20% `checked_out` = 12 exactly, zero `missing_lost`, zero
`reserved_waitlist` (waitlists start only via UI).

## 2. `mocks/building.json` Schema

```json
{
  "building": { "code": "MAIN", "name": "Athenaeum Main Building", "address": "1 Arsenal Library Way" },
  "floors": [
    {
      "floor_number": 1,
      "name": "Reading Hall",
      "sections": [
        {
          "code": "SCI",
          "name": "Science, Mathematics & Tech",
          "color": "#10B981",
          "aisles": [
            { "code": "A-1", "shelves": [ { "code": "S-1", "row_level": 3, "height_tier": "Mid Stacks" }, { "code": "S-2", "row_level": 4, "height_tier": "Eye Level" } ] }
          ]
        }
      ]
    }
  ]
}
```

| Field | Rules |
| --- | --- |
| `building.code` | `MAIN` (single building) |
| `floor.floor_number` | int ≥ 0; `0F` allowed for lobby-only floors |
| `section.code` | one of `SCI, PER, HVA, REF, SF` — categories & colors: [05 §2](05-building-layout.md#2-section-categories) |
| `section.color` | must equal the canonical hex in that table (validated by seed) |
| `aisle.code` | `A-{n}` unique within section |
| `shelf.code`, `row_level`, `height_tier` | `S-{n}` unique within aisle; `height_tier` free text ("Eye Level", "Mid Stacks", "Low Stacks", "Tower Top") |

Concrete hierarchy values: [13 §shelf inventory](13-mock-data-reference.md#4-shelf-inventory-all-40).

## 3. `mocks/books.json` Schema

```json
{
  "books": [
    {
      "title": "Dune",
      "subtitle": null,
      "authors": ["Frank Herbert"],
      "isbn13": "9780441013593",
      "publisher": "Ace Books",
      "published_year": 1965,
      "description": "Having upset the subtle ecology ...",
      "section_code": "SF",
      "open_library_key": null,
      "copies": [
        { "barcode": "ATH-002941-C1", "status": "available", "condition": "good", "shelf_path": "MAIN/3F/SF/A-1/S-2" },
        { "barcode": "ATH-002941-C2", "status": "available", "condition": "good", "shelf_path": "MAIN/3F/SF/A-1/S-2" },
        { "barcode": "ATH-002941-C3", "status": "available", "condition": "fair", "shelf_path": "MAIN/3F/SF/A-1/S-2" }
      ]
    }
  ]
}
```

| Field | Rules |
| --- | --- |
| `isbn13` | optional; when present must pass EAN-13 checkdigit (validated by seed via `pkg/checkdigit`) |
| `section_code` | must match a seeded section code |
| `copies[].barcode` | optional — auto-allocated when omitted ([§5](#5-barcode-allocation)); format `ATH-[0-9]{6}-C[0-9]{2}` ([02](02-data-model.md#book_copies)) |
| `copies[].status` | `available` \| `checked_out` \| `missing_lost`; `checked_out` needs no borrower in mocks |
| `copies[].shelf_path` | resolvable path ([05 §4](05-building-layout.md#4-wayfinding-resolution)) |
| `copies: []` | zero-copy book (5 of them — [13 §test tables](13-mock-data-reference.md#5-edge-case-tables)) |

## 4. Seed Algorithm

`cmd/seed` (flags: `--mocks=./mocks`, `--reset`, `--offline`):

```bash
1. load config; connect Postgres (DATABASE_URL)
2. seed users first (SEED_ADMIN_EMAIL + SEED_USER_EMAIL; argon2id passwords — docs/04 §6)
3. upsert building tree in one transaction:
     for floor → section (color validated) → aisle → shelf  (natural-key upsert:
     conflict on (parent_id, code)); collect map[f-code]path → shelf_id
4. for each book (ordered as in the file):
     a. validate isbn13 checkdigit when present
     b. resolve section_id from section_code; upsert by isbn13 else title+first author
     c. for each copy: resolve shelf_path → shelf_id (unknown path = hard failure,
        see §8); allocate barcode if omitted (§5); INSERT/ON CONFLICT (barcode) DO UPDATE
     d. append copy_events row per copy (from_status NULL → status, source 'seed')
5. print counts: users, books, copies, shelves — expect 2/40/60/40 (docs/13 §1)
6. remind: 'cd .. && go run ./cmd/reindex' (embeddings, docs/06 §2)
```

- **Deterministic**: file order is insertion order; no randomness, no timestamps
  newer than the run.
- The same resolver as admin APIs is reused (`internal/service/building.ResolvePath`)
  — [05 §4](05-building-layout.md#4-wayfinding-resolution).

## 5. Barcode Allocation

Accession numbers are a single building-wide counter so the DESIGN.md example
(`ATH-002941-C1`, book #1 of the catalog — [13](13-mock-data-reference.md)) stays fixed:

```
accession(counter) = 002940 + book_index          # book 1 → 002941
book.book_copies: copy n → ATH-{accession}-C{n}    # n zero-padded to 2
```

- Explicit `barcode` in mocks for book #1 Dune (`ATH-002941-C1..C3`) — seed
  cross-checks generated vs explicit values and errors on drift.
- Admin-created books continue the same counter (`POST /api/admin/copies` without
  barcode — [03 §8.3](03-api-spec.md#83-admin-copies)).

## 6. Open Library Fallback

Used by `cmd/seed` (enrichment) and `POST /api/books/fetch-isbn`
([03 §5.3](03-api-spec.md#53-post-apibooksfetch-isbn)); client code lives in
`internal/service/olbooks/`.

| Case | Behavior |
| --- | --- |
| Found | title/publisher/year/description/covers/Dewey/LOC normalize → returned/enriched; cover = `covers.openlibrary.org/b/isbn/{isbn13}-L.jpg` |
| Not found (404) | keep local mock values; `open_library_key` stays NULL |
| Timeout/error | 3 s timeout, 2 retries (500 ms backoff), then fallback below |
| All upstream failed | seed: proceed with mock values, log `WARN enrich_failed`; `fetch-isbn`: return 200 partial (`source:"offline_partial"`, `warnings:["upstream_unavailable"]` — intake never blocks on the network) |
| `OFFLINE_MODE=true` | skip all lookups; behave as "all upstream failed" |

Mock descriptions in `books.json` are the offline source of truth — real texts for
better-known works, neutral editorial blurb templates for archive items
([13 §catalog](13-mock-data-reference.md#2-book-catalog-all-40)).

## 7. Reindex

`cmd/reindex` (flags: `--model-check`, `--book={id}`):

```bash
1. truncate nothing; upsert per book:
   for each book → chunk description (docs/06 §2 parameters) →
   batch 8 sizes embedding via Ollama → INSERT ... ON CONFLICT (book_id, chunk_index)
2. --model-check only: report books whose rows have (model, chunker_version) ≠ current
3. exit summary: indexed_books, chunks_written, stale_chunks
```

Trigger manually after changing `OLLAMA_EMBED_DIM`/model or `chunker_version`
(columns documented in [02 §book_embeddings](02-data-model.md#book_embeddings)).

## 8. Failure Modes

| Failure | Detection | Action |
| --- | --- | --- |
| Unknown `shelf_path` | resolver returns 404 | hard abort + line number (bad fixture, not code) |
| Invalid ISBN checkdigit | `pkg/checkdigit` | hard abort |
| Section color mismatch vs [05 §2](05-building-layout.md#2-section-categories) | seed validation | hard abort |
| Duplicate explicit barcode | PG unique violation | hard abort (fixture bug) |
| Duplicate ISBN across books | PG partial unique | hard abort |
| Postgres down | conn refused | exit non-zero before any write |
| Partial run (mid-transaction crash) | transaction semantics | rollback — tree step is one TX per floor-group |
| Ollama down during reindex | conn refused after 10 s | exit code 3, no partial writes (single prepared batch) |

## 9. Idempotency & Reset

- Re-running is safe: every upsert is keyed by natural keys, so counts stay
  identical; only `updated_at` and seeded `copy_events` rows change.
- `--reset` truncates `book_copies, copy_events, book_embeddings, user_library,
  waitlist, books, conversations, messages, shelves, aisles, sections, floors,
  buildings` (children first, `users`/`refresh_tokens` untouched) then reseeds.
