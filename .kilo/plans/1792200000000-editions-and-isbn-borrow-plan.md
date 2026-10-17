# Plan — One book / many ISBNs (editions) + borrow & return by ISBN

## Findings
- DB check: two Project Hail Mary rows — seeded `9780593135204` (2 copies) and staff-added
  `9780593355275` (1 copy). Not a bug in uniqueness: `books_isbn13_key` (docs/02 §9) makes one ISBN = one row,
  and an ISBN identifies an **edition** (hardcover / paperback / reprint), so the same title can
  legitimately arrive under a new ISBN. Copies are already distinguished by copy tag `ATH-…-CY`.
- Scan by ISBN yields `match:"book"` with no copy → no Borrow/Return today (copy-tag only).
- Decisions below use the recommended defaults (the clarifying questions were dismissed); tell me to flip any.

## A. Model: one catalog book, many ISBNs
- New table `book_isbns(isbn13 TEXT PRIMARY KEY, book_id UUID REFERENCES books ON DELETE CASCADE,
  is_primary BOOL, created_at)`; `books.isbn13` stays the primary ISBN (display/back-compat).
  Migration backfills one primary row per book; partial-unique "one primary per book".
- Every ISBN lookup goes through `book_isbns`: `POST /api/books/scan`, `fetch-isbn` conflict check
  (`BookIDByISBN`), `identify-isbn` (`in_catalog`/`book_id`), admin create uniqueness, keyword search
  (searching an alt ISBN finds the book). Detail (`GET /api/books/{id}`) gains `isbns: [{isbn13,is_primary}]`.
- Admin: `POST /api/admin/books/{id}/isbns {isbn13}` (attach an edition, checksum-validated, 409
  `ISBN_CONFLICT` + book_id if owned by another book) and `DELETE …/isbns/{isbn13}` (non-primary only).
- Admin merge `POST /api/admin/books/{id}/merge {into}` (one tx): move copies, `user_library`
  (conflict → keep higher status/rating), waitlist, `copy_events`, ISBNs; delete the source; mark
  `embedding_dirty` for the target; audit row. Use it once to merge the Project Hail Mary pair
  (canonical = seeded `6bb2ef05…`, keeps both ISBNs, 3 copies).

## B. Staff intake dedupe (web + API)
- `fetch-isbn` response gains `similar: [{id,title,authors,isbn13,copies}]` — books whose normalized
  title equals the looked-up title and share ≥1 author (trigram not required in v1).
- Add-book form: when `similar` is non-empty show a banner "Looks like *Project Hail Mary* is already
  in the catalog" with **Add as another edition of this book** (attach ISBN + create copies on the chosen
  shelf, no new book row) and **Create a separate book anyway**. Section/shelf stay required; section
  pre-filled from the matched book. Test ids `intake-similar`, `intake-attach`, `intake-create-new`.
- Book detail shows an "Editions" list (ISBNs) for staff-visible context.

## C. Borrow / return by ISBN (needed for physical testing without ATH tags)
Builds on M17 (borrow/renew/return/loans — backend subagent currently implementing; keep copy-tag paths).
- Scan `match:"book"` result gains `borrowable` (≥1 available copy, or caller holds a ready hold) and
  `borrow_copy: {id, barcode, location}` = the copy that **would** be loaned (lowest available tag;
  caller's own `reserved_waitlist` first), plus `returnable` + `my_loans: [{id,copy_id,barcode,due_at}]`
  for that book.
- New endpoints (same rules: 14d, max 5, trust-based, 10/min bucket shared with copy borrow):
  `POST /api/books/{id}/borrow` — server picks the copy (`FOR UPDATE SKIP LOCKED`, lowest tag), returns
  `{copy, loan}` and the UI tells the patron which tag/shelf to take; `POST /api/books/{id}/return` —
  closes the caller's oldest open loan of that book (409 `CONFLICT` if none). Copy-tag borrow/return stay.
- Scanner UI: ISBN book match shows **Borrow this book** (`scan-borrow`, shows the chosen tag + shelf
  and due date after success) and **Return this book** (`scan-return`) when the caller has a loan;
  replaces the "scan the copy tag" hint (keep it only when nothing is borrowable). i18n en + zh-CN.

## D. Docs (first commit)
docs/02 (`book_isbns`, indexes, merge), 03 (§5.2 `isbns`, §5.3 `similar`, §5.4 scan fields,
new borrow/return-by-book + admin isbns/merge sections, rate table), 11 (§2b-ii ISBN borrow flow,
§7/§8), 07, 12 (editions banner, scanner), 13 (alt ISBN fixture for PHM), 14, 10 (item 20), TODO Phase 21.

## E. Tests
Go: migration up/down + backfill; ISBN resolution via alt ISBN (scan/search/identify/conflict); attach /
merge (copies, library conflicts, waitlist); `similar`; borrow-by-book picks lowest available, honours own
hold, limit, concurrency (two borrowers, last copy); return-by-book. Vitest: scan view helpers.
Playwright: scan alt ISBN resolves to the same book; borrow by ISBN → due date + chosen tag → return by ISBN;
intake similar banner → attach edition (cleanup deletes the ISBN).

## Order / commits
docs → db+api editions → api borrow-by-book → web intake → web scanner. Gates: `make lint test e2e`.
Out of scope: automatic fuzzy dedupe/merge, ISBN-10, FRBR work/edition hierarchy, fines.
