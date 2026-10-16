<!-- Copy everything below into the UI agent. Repo: /Users/senohebat/repo/athenaeum -->

# Task — UI for hybrid-default search, rating averages, and scan-based borrow/return (with a Loans tab)

You are working in the **Next.js app only** (`web/`) of the Athenaeum monorepo at
`/Users/senohebat/repo/athenaeum`. Do **not** change `api/`; the Go endpoints below are the
contract and are being built in parallel.

## Read first (the docs are the contract)

1. `AGENTS.md` (ground rules, doc precedence).
2. `docs/03-api-spec.md` — §5.1 (search + `meta.mode_used`/`meta.degraded`/`index_pending`,
   `rating_avg`/`rating_count`), §5.2 (detail rating fields), §5.4 (scan result
   `borrowable`/`loan`), §6.4 (return), **§6.5 borrow**, **§6.6 renew**, **§6.7 `GET /api/loans`**.
3. `docs/07-frontend.md` — the `/catalog`, `/library` and Scanner-dialog sections (updated for
   this work).
4. `docs/12-ui-design.md` — Screen 2 (catalog), Screen 4 (library), §10 copy state machine.
5. **Refreshed Stitch artifacts (2026-10-16)** — visual source of truth for the scanner and the
   library page: `.artifacts/stitch/barcode_isbn_scanner_athenaeum_library/` (screen.png + code.html)
   and `.artifacts/stitch/my_library_athenaeum_library/` (screen.png + code.html). These are
   **superset sketches** — implement only what the docs below say, not every affordance they draw.
6. `docs/14-localization.md` — §1 scope (patron screens + scanner Patron Lookup are localized;
   **admin stays English**), §5 message catalog, §6 enum label maps.
6. `docs/11-barcode-scanning.md` — §7 UI spec, §8 error table.

Follow existing conventions: server components for reads, **URL-as-state**, ShadCN + Tailwind
tokens from `docs/12`, `next-intl` keys with **en + zh-CN parity**, no user-facing literals in
components.

## Feature A — Hybrid search by default (no visible mode tabs)

**Behaviour**
- `/catalog` has **one search box**; the search runs in **hybrid** mode by default. Patrons must
  not be asked to pick a search type.
- **Remove the always-visible mode tabs** (`mode-keyword|semantic|hybrid` in
  `web/components/catalog-controls.tsx`). Move Keyword/Semantic behind an **Advanced** disclosure
  (e.g. a `Disclosure`/popover or a small "Advanced" toggle revealing the mode control). The
  `mode` URL param stays supported.
- **Empty query = browse**: with an empty box, show the keyword listing (all books). Do not call
  the embedding path and do not show an error.
- **Embedding outage degrades silently**: the API returns `200` with
  `meta.mode_used:"keyword"` and `meta.degraded:true` instead of `503`. When `meta.degraded` is
  true and the user asked for semantic/hybrid explicitly (Advanced), show a subtle,
  non-blocking note — do **not** show the old "semantic unavailable" error card on the default path.
- Keep the existing empty-query idle prompt (`catalog-query-required`, `requiresQuery()` in
  `web/lib/catalog-query.ts`) only for the case where a user **explicitly** forces
  `mode=semantic|hybrid` (via Advanced or a deep link) with an empty box.

**Contract deltas to consume** (types in `web/lib/types.ts`)
- `SearchMeta` gains `mode_used: "keyword" | "semantic" | "hybrid"` and `degraded: boolean`
  (alongside the existing `index_pending`).
- Search items and book detail gain `rating_avg: number | null` and `rating_count: number | null`.

## Feature B — Rating average on cards and detail

- Show `★ {avg} ({count})` from `rating_avg`/`rating_count`, **1 decimal** (the API already
  rounds), e.g. `★ 4.6 (12)`.
- Render on the catalog `BookCard` (`web/components/book-card.tsx`) and on `/books/[id]`.
  Reuse/extend `web/components/star-rating.tsx` for the static (non-interactive) display; the
  existing editable `StarRating` on the library/detail "my rating" control stays as-is.
- **Hidden when unrated** (`rating_avg === null || rating_count === 0`): show nothing, or the
  localized "No ratings yet" where a slot needs to stay stable (detail page).
- Add i18n keys (en + zh-CN): `catalog.card.rating` = `★ {avg} ({count})`,
  `book.rating.none`, and a label for the **Advanced** reveal (e.g. `catalog.search.advanced`).

## Feature C — Scan-based borrow / return + Loans tab

**Scanner dialog — Patron Lookup** (`web/components/barcode-scanner/patron-lookup-panel.tsx`)

> The refreshed scanner artifact draws a **three-way Borrow / Return / Add to DB** selector. **Do not
> adopt it** — keep the existing **two tabs** (Patron Lookup / Staff Intake) and put Borrow/Return as
> actions inside Patron Lookup (decided 2026-10-16). Do **not** add ISBN-10 support, RFID encode or
> Print Tag. Loan terms are **14-day, max 5 open loans** (not the artifact's "21-day / quota 6").
- The scan result already renders a copy for a copy-tag match. Use the new fields:
  - `returnable` → **Return this copy** → `POST /api/copies/{id}/return`.
  - `borrowable` → **Borrow this copy** → `POST /api/copies/{id}/borrow`; on success show a
    toast/toast-with-due-date ("Borrowed — due {date}") and refresh the result to the new
    state (`checked_out`).
  - `loan` (open loan: `{id,user_id,due_at,renewals,mine}`) → when present, show the copy's
    status and, if `mine`, the due date.
- Suggested test ids (extend the existing `scan-*` set): `scan-borrow`, keep `scan-return`;
  `scan-due`. Keep `scan-next`, `scan-view-record`, `scan-waitlist` behaviour unchanged.
- Handle the new error codes: `409 CONFLICT` (not available / not checked out),
  `409 BORROW_LIMIT` ("You already have 5 loans"), `403 FORBIDDEN` (renew someone else's),
  `429 RATE_LIMITED` (borrow 10/min, renew 5/min) — follow `docs/11 §8`.

**`/library` — Loans tab** (`web/app/(shell)/library/page.tsx`)
- Add a **Loans** stage/tab (reuse the existing stage-tab mechanism; test id `tab-loans`) that
  lists the caller's loans from `GET /api/loans` (`web/lib/api-client.ts`).
- Each row shows: book (title, cover thumbnail), copy `barcode`, `borrowed_at`, `due_at`
  (localized), and:
  - an **overdue** badge when `overdue: true`;
  - **Renew (once)** when `renewable: true` → `POST /api/copies/{id}/renew`, then refresh;
  - **Return** → `POST /api/copies/{id}/return`, then refresh;
  - history rows (`kind:"past"`) show `returned_at` and no actions.
- Reuse the shared `ConfirmDialog` for the return action (destructive-adjacent) and the
  existing toast conventions; see `docs/07 §9`.
- Suggested test ids: `loans-list`, `loan-row`, `loan-due`, `loan-overdue`, `loan-renew`,
  `loan-return`. Reuse the existing `library-scan-return` entry point.
- i18n keys (en + zh-CN): `library.tabs.loans` + `library.loans.{empty,due,dueSoon,overdue,renew,renewed,returned,scanHint}`.
- Do **not** add fines, reminders, or pickup notifications (out of scope, `docs/10 §2`).

## Feature D — Library extras (from the refreshed My Library artifact)

Per `docs/12` Screen 4 and `docs/07 §/library`:

- Stage selector gains a fourth stage **Dropped** (new `user_library.status` value — the backend adds
  it; render the tab and the selector option; seed has none so the tab shows an empty state).
- Header actions: **Scan to Return** (existing), **Export History** (client-side CSV of the library +
  loans — no backend call), **Add ISBN Entry** (👑 **admin-only**; hide for patrons — patrons scan
  instead), **Browse Catalog**.
- Toolbar: saved-books search, **In Library Building** filter, **Sort** (Recently Added / Title /
  Due date) and a **list/grid** view toggle — all URL-as-state.
- Past-loan rows get a client-side **Loan Receipt** (printable summary). **No review text** (numeric
  rating only).
- Suggested test ids: `library-export`, `library-view-toggle`, `library-sort`, `loan-receipt`.

## API client additions (`web/lib/api-client.ts`, `web/lib/types.ts`)

- `searchBooks` / types: add `mode_used`, `degraded` to the meta type; `rating_avg`,
  `rating_count` to the item/detail types.
- `borrowCopy(copyId)`, `renewCopy(copyId)`, `returnCopy(copyId)` (return may already exist),
  `listLoans()`. Mirror the documented response shapes exactly.

## Tests

- **Vitest** (`web/lib/*.test.ts`, `pnpm -C web test`): new helpers (e.g. `requiresQuery` stays;
  add coverage for any new pure logic such as due-date formatting / `renewable` mapping / rating
  display formatting) and keep the i18n **key-parity** test green.
- **Playwright** (`web/e2e/`, Chromium, use **`gotoReady()`** and `switchLang()` from
  `web/e2e/helpers.ts`; call `resetRateLimits()` from `web/e2e/rate-limit.ts` in `beforeAll`
  for request-heavy specs):
  - `/catalog`: default (no mode chosen) search returns results without the mode tabs visible;
    Advanced reveals the mode control; a book card shows `★ x.y (n)`; assert the rating is
    hidden for an unrated book.
  - `/catalog` degraded path: mock the search response with `page.route` (server-rendered HTML
    can't be intercepted by route for the RSC fetch — prefer a client-side assertion or assert
    the non-blocking note element once the client re-check runs; if a server-render mock is
    needed, follow how `web/e2e/index-status.spec.ts` handles it).
  - Scanner (manual code path, camera never runs in CI): enter a copy tag for an `available`
    copy → **Borrow this copy** → assert the due date is described; then **Return this copy**.
    Update the existing `web/e2e/scanner.spec.ts` expectations that assume return-only.
  - `/library` Loans tab: renew once (button disappears / `renewable` false) and return.
  - `/library` extras: Dropped tab shows the empty state; sort/view toggle changes the layout and
    URL; Export History downloads a CSV; Add ISBN Entry is absent for the demo patron and present
    for an admin; Loan Receipt renders for a past loan.
  - **Update `web/e2e/catalog.spec.ts`**: it currently clicks the visible mode tabs
    (`mode-semantic`, `mode-hybrid`); retarget those through the new **Advanced** control.
  - The e2e DB is seeded with the 20 reviewer accounts and loans, so ratings and a Loans tab
    have data.

## Definition of done

- `pnpm -C web lint` (eslint + `tsc --noEmit`), `pnpm -C web test`, `pnpm -C web build` pass.
- `make e2e` passes (backend must be in place; if the backend isn't landed yet, run against
  `page.route` mocks for endpoints that don't exist and say so explicitly in your report).
- No user-facing literals outside `web/messages/{en,zh-CN}.json`; both catalogs key-for-key.
- Admin screens untouched (and still English). No changes under `api/`.
- Prettier-clean (the pre-commit hook runs `prettier --check` — run
  `pnpm -C web exec prettier --write` on files you touched).

## Out of scope

Fines/reminders/pickup notices, RFID/label/MARC, rating histograms or review text, changes to
admin screens, any new backend endpoint beyond §5/§6 above.

## Report back

List files changed, the exact test ids and i18n keys you added, what you mocked vs. exercised
against the real API, and the commands you ran with their results.
