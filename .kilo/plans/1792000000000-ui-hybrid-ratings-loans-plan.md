# Plan — Hybrid default search, rating averages, scan borrow/return + Loans tab

Source of truth: `.kilo/prompts/ui-hybrid-ratings-loans.md`, impl plan M15/M17, TODO Phases 16/18,
docs/03 §5.1/§5.2/§5.4/§6.4-6.7, docs/07, docs/11, docs/12, docs/14.
Status check: TODO Phase 16/18 items are all unchecked, so the **API is not built** (no `loans`
table; migrations stop at 0006). Web-only scope below; API steps are prerequisites.

## 0. Prerequisites (API, order matters; separate commits)
1. `feat(db)` migration 0007 `book_ratings` view; 0008 `loans` table + drop `book_copies.due_at`.
2. `feat(api)` search: default hybrid, empty q browses, embed failure -> 200 keyword with
   `meta.mode_used`/`meta.degraded`; `rating_avg`/`rating_count` on items + detail.
3. `feat(api)` borrow/renew/return/`GET /api/loans`; scan result `borrowable`/`returnable`/`loan`.
4. Seed (M16): 20 reviewer accounts, ratings, one loan per checked_out copy (so e2e has data).
If the API is deferred, web work proceeds against `page.route` mocks (state this in the report).

## 1. Shared types / client (`web/lib`)
- `types.ts`: `SearchMeta` += `mode_used`, `degraded`; item/detail += `rating_avg`, `rating_count`
  (nullable); scan copy += `borrowable`, `returnable`, `loan`; new `Loan` type.
- `api-client.ts`: `borrowCopy`, `renewCopy`, `returnCopy` (reuse if present), `listLoans`.
- `api-errors.ts`: map `BORROW_LIMIT`, borrow/renew `RATE_LIMITED`, renew `FORBIDDEN` to i18n keys.
- New pure helpers + vitest: `rating.ts` (`formatRating`, `hasRating`), `loans.ts`
  (due-date formatting, overdue/dueSoon, renewable). Keep i18n key-parity test green.

## 2. Feature A — Hybrid default (`catalog-controls.tsx`, `catalog/page.tsx`, `catalog-query.ts`)
- Remove always-visible mode tabs; default `mode` = hybrid when URL param absent.
- Add **Advanced** disclosure (toggle, `catalog-advanced`) exposing Keyword/Semantic (+Hybrid) with
  the existing `mode-*` test ids; `mode` URL param still honored; disclosure auto-open if mode in URL.
- Empty box = browse: page requests keyword listing, no idle prompt, no error. `requiresQuery()`
  only for explicit `mode=semantic|hybrid` + empty q (deep link/Advanced).
- Degraded: when `meta.degraded`, render subtle muted non-blocking note (`catalog-degraded-note`),
  only when mode was explicitly chosen; no error card on default path. Drop 503 error-card reliance
  for EMBEDDING_UNAVAILABLE on the happy path (keep as fallback).
- Fix the root cause noted earlier: mode switch with empty q must not produce a 400.

## 3. Feature B — Ratings (`book-card.tsx`, `star-rating.tsx`, `books/[id]/page.tsx`)
- Static read-only `RatingSummary` (`★ 4.6 (12)`), hidden on card when unrated; detail shows
  localized "No ratings yet" to keep layout stable. Test ids `book-rating`, `book-rating-none`.
- i18n en+zh-CN: `catalog.card.rating`, `book.rating.none`, `catalog.search.advanced`,
  `catalog.degraded.note`.

## 4. Feature C — Borrow/return + Loans
- `patron-lookup-panel.tsx` (keep two tabs): by state show `scan-borrow` / `scan-return`, `scan-due`
  (due date when `loan.mine`), status text when someone else holds it. After success: toast
  "Borrowed — due {date}", refresh result to new state. Error mapping per docs/11 §8.
- `library/page.tsx`: add `tab-loans` stage (URL-as-state) fetching `listLoans()`; new
  `loans-list.tsx` component: rows `loan-row` (cover, title, barcode, borrowed_at, `loan-due`,
  `loan-overdue` badge, `loan-renew` if renewable, `loan-return` via shared `ConfirmDialog`);
  past rows show returned_at, no actions. Refresh via router.refresh.
- i18n: `scanner.borrow*`, `library.tabs.loans`,
  `library.loans.{empty,due,dueSoon,overdue,renew,renewed,returned,scanHint}`.
- No fines/reminders/notices; admin screens untouched.

## 5. Tests
- Vitest: rating + loans helpers, catalog-query default mode, parity.
- Playwright (`gotoReady`, `resetRateLimits` in beforeAll): update `catalog.spec.ts` (mode tabs ->
  via Advanced); new default-search, rating shown/hidden, degraded note (client-side or like
  `index-status.spec.ts`); update `scanner.spec.ts` (borrow -> due -> return); Loans tab renew-once
  + return in `library.spec.ts`.

## 6. Delivery (one conventional commit per feature, scope `web`)
1. `feat(web): hybrid default search with advanced mode disclosure`
2. `feat(web): rating average on cards and detail`
3. `feat(web): scan borrow/return actions`
4. `feat(web): library loans tab`
Gate each: `pnpm -C web lint`, `pnpm -C web test`, prettier --write on touched files; before final
`make lint && make test && make e2e` and `pnpm -C web build`. Tick TODO.md Phase 16/18 web items +
handoff row. Out of scope: M18 extras (Dropped, export, receipt, view/sort).
