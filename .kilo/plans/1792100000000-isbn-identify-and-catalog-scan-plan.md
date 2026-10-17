# Plan — Identify unknown ISBNs (Open Library) + scan button in catalog search

## Answer: public ISBN API
Yes — **Open Library** (Books API `GET /api/books?bibkeys=ISBN:{isbn}&jscmd=data&format=json`, covers at
`https://covers.openlibrary.org/b/isbn/{isbn}-L.jpg`). It is already integrated: `olbooks` client
(3s timeout, 2 retries), Redis `ISBNCache` (24h), `ISBNService`, `OPENLIBRARY_BASE_URL`, `OFFLINE_MODE`,
and an e2e stub (`api/cmd/e2e-stubs/openlibrary.go`, `StubISBN=9780553293357` Foundation, not in the
seeded catalog). Today it is only reachable via admin-only `POST /api/books/fetch-isbn`; patron
`POST /api/books/scan` is documented "local only, never hits Open Library", so a valid-but-unknown
ISBN (e.g. 9780593355275) shows only the generic "Not in this building's catalog" card.

## Decision (docs own endpoints — update docs first)
Keep `/api/books/scan` pure/local and fast. Add a **patron-safe** endpoint
`POST /api/books/identify-isbn` (🔒 any user, rate limit 10/1min, new bucket `identify_isbn`),
called by the scanner **only after** scan returns `match:"none"` + `code_type:"isbn13"`. The
"not in library" card renders instantly, then fills in the identified book (no scan latency, OL
traffic throttled independently). Alternative rejected: embedding OL in `/scan` (adds up to 3s to
every miss, shares the 30/min scan bucket, breaks the documented invariant).

Response 200 (never 409, never 5xx for upstream trouble):
```json
{ "isbn13":"9780593355275", "in_catalog":false, "book_id":null,
  "identified": {"title":"…","authors":["…"],"publisher":"…","published_year":2022,"cover_url":"https://covers.openlibrary.org/b/isbn/…-L.jpg"} ,
  "source":"openlibrary", "warnings":[] }
```
`identified:null` + `source:"offline_partial"` + `warnings:["not_found"|"upstream_unavailable"]` when
OL has no record / is down / `OFFLINE_MODE=true`. If the ISBN meanwhile exists locally →
`in_catalog:true` + `book_id` (UI offers "View Full Record"). Invalid checksum → 400 `INVALID_ISBN`.

## Step 1 — Docs (docs-only commit `docs: ...`)
- docs/03: new §5.5 `identify-isbn`; fix §5.3 note ("patron never hits OL" → patrons use §5.5) and the
  rate-limit table §2 (+ `identify_isbn` 10/min).
- docs/11: §2a add identify step (sequence diagram), §7 Result — "not in library" row (warning +
  cover/title/authors), §8 rewrite the `in_catalog:false` row, §9 endpoint table, §11 scope.
- docs/07 (scanner dialog + `/catalog` search bar scan button), docs/12 (Screen 2 scan affordance,
  scanner not-in-catalog state), docs/14 (new keys), docs/01 §integration row (OL now also patron
  identification), docs/09 §8.1 (stub reused; update playwright comment), docs/10 §1 new item 19, TODO.md
  new Phase 20 + plan M19.

## Step 2 — API (`feat(api)`)
- `model`: `ISBNIdentification` / `IdentifyResult`.
- `service/catalog/isbn.go`: `ISBNService.Identify(ctx, raw)` = `ValidateISBN` → `repo.BookIDByISBN`
  (found ⇒ in_catalog) → shared `cached()`/`lookup`/`store()` (same Redis cache as intake) → map to
  patron fields (omit dewey/loc/description). Reuse `partial()` warnings.
- `handler/isbn.go`: `Identify` handler; `server.go`: `POST /books/identify-isbn` with
  `requireAuth, limit(middleware.BucketIdentifyISBN)` (+ add to the route table/rate list); wire in `cmd/api`.
- `middleware/ratelimit.go`: `BucketIdentifyISBN{Limit:10,Window:1m,ByUser:true}`.
- Tests: service (identified, not_found, upstream down, offline, in-catalog race, invalid ISBN, cache
  hit), handler envelope, rate limit, integration test through the stub.

## Step 3 — Web scanner (`feat(web)`)
- `lib/types.ts`: `IdentifyResponse`; `lib/api-client.ts`: `identifyIsbn(isbn)`.
- `lib/scan.ts`: `shouldIdentify(result)` (isbn13 + match none) + pure view-state helper (vitest).
- `patron-lookup-panel.tsx`: for the not-in-catalog card, after scan miss call `identifyIsbn` once
  (abort on Scan Next/new code); states: loading skeleton → **identified** (cover via `BookCover` using
  OL `cover_url`, title, authors, publisher·year) / **unidentified** (today's generic text).
  In both, keep a persistent warning banner (`role="status"`, destructive/amber tone, test id
  `scan-not-registered`): "Not registered in this library — this title isn't in the catalog." Staff
  keeps "Add this book (Staff Intake)"; `in_catalog:true` swaps the warning for "View Full Record".
  Test ids: `scan-identified`, `scan-identified-title`, `scan-identified-cover`, `scan-not-registered`.
- Cover image: allow `covers.openlibrary.org` in `next.config.ts` images/remotePatterns (check how
  `BookCover` loads remote covers) and keep the typographic fallback on error.
- i18n en + zh-CN: `scanner.notInCatalog.warning`, `.identifying`, `.identifiedBy`, `.unidentified`,
  `.alreadyAdded`; parity test stays green. Admin UI untouched.

## Step 4 — Scan button in `/catalog` search bar (`feat(web)`)
- `catalog-controls.tsx` `CatalogSearchBar`: icon button inside the search input (right side, next to
  the clear ×), `data-testid="catalog-scan"`, `aria-label` `catalog.search.scan`, calls the existing
  `openScanner("patron")` (shared `ScannerHost`; camera/manual input unchanged). Result flows through
  the same Patron Lookup panel (found → View Full Record & Map; unknown → identified + not-registered
  warning). Input right padding adjusted so the clear × and scan icon don't overlap.
- i18n `catalog.search.scan` (en + zh-CN).

## Step 5 — Tests
- Vitest: `shouldIdentify`, view-state helper, key parity.
- Playwright (`scanner.spec.ts`, manual input only): `9780553293357` (stub Foundation) → card shows
  `scan-not-registered` + title "Foundation" + author; an ISBN the stub doesn't know (valid checksum,
  e.g. `9780593355275`) → warning + generic text, no error toast; `catalog.spec.ts`: `catalog-scan`
  opens the dialog (`scanner-manual-input` visible), localized in zh-CN. `resetRateLimits()` in beforeAll.

## Delivery
Commits: docs → api → web scanner → web catalog scan button (Conventional, scope `web`/`api`).
Gates: `make lint`, `make test`, `pnpm -C web build`, `make e2e`; update TODO.md.
Out of scope: ISBN-10 support, auto-adding patron-requested books, purchase requests, writing OL data
to the DB, Google Books fallback.
