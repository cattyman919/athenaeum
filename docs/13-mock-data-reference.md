# Mock Data Reference

> Concrete catalog of every seeded fixture: all 40 mock books (with checksum-valid ISBN-13s and home shelves), the complete 40-shelf inventory, edge-case tables (zero-copy, multi-copy, checked-out ledger), demo users, and how to extend the data — schemas in [08-seed-data.md](08-seed-data.md), tables in [02-data-model.md](02-data-model.md).

## Contents

1. [Fixture Totals](#1-fixture-totals)
2. [Book Catalog (all 40)](#2-book-catalog-all-40)
3. [Copy Status Ledger](#3-copy-status-ledger)
4. [Shelf Inventory (all 40)](#4-shelf-inventory-all-40)
5. [Edge-case Tables](#5-edge-case-tables)
6. [Demo Users & Personal Library](#6-demo-users--personal-library)
7. [Extending the Mock Data](#7-extending-the-mock-data)

## 1. Fixture Totals

| Metric | Value |
| --- | --- |
| Buildings / floors / sections / aisles / shelves | 1 / 3 / 5 / 15 / 40 |
| Books | 40 |
| Copies | 60 (12 `checked_out` = 20%, 48 `available`, 0 `missing_lost`, 0 `reserved_waitlist`) |
| Zero-copy books | 5 |
| Users | 22 (`admin`, demo, and 20 reviewers `user1@gmail.com`..`user20@gmail.com`) |
| `user_library` rows | 24 for demo (2 reading · 15 read · 7 want_to_read; 7 favorites) + ~200 reviewer rating rows |
| `loans` rows | 12 (one open loan per seeded `checked_out` copy, owned by reviewers; all due dates in the future) |
| `waitlist` rows | 2 (demo patron) |

## 2. Book Catalog (all 40)

Order = insertion order (`books.json`); `accession = 002940 + #` → barcode roots
(`ATH-002941-…` book #1 — matches the DESIGN.md/Stitch copy manifest).
All ISBN-13s pass EAN-13 checkdigit. Home `shelf_path` applies to all copies of the
book unless noted in [§3](#3-copy-status-ledger).

| # | Title | Author(s) | Year | Section | ISBN-13 | Copies | Home shelf path |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | Dune | Frank Herbert | 1965 | SF | `9780441013593` | 3 | `MAIN/3F/SF/A-1/S-2` |
| 2 | Neuromancer | William Gibson | 1984 | SF | `9780441569595` | 3 | `MAIN/3F/SF/A-1/S-2` |
| 3 | Hyperion | Dan Simmons | 1989 | SF | `9780553283686` | 2 | `MAIN/3F/SF/A-1/S-3` |
| 4 | The Left Hand of Darkness | Ursula K. Le Guin | 1969 | SF | `9780441478125` | 2 | `MAIN/3F/SF/A-2/S-1` |
| 5 | Foundation | Isaac Asimov | 1951 | SF | `9780553380019` | 3 | `MAIN/3F/SF/A-2/S-1` |
| 6 | Snow Crash | Neal Stephenson | 1992 | SF | `9780553380958` | 3 | `MAIN/3F/SF/A-2/S-2` |
| 7 | The Three-Body Problem | Liu Cixin | 2008 | SF | `9780765382030` | 2 | `MAIN/3F/SF/A-2/S-3` |
| 8 | Project Hail Mary | Andy Weir | 2021 | SF | `9780593135204` | 2 | `MAIN/3F/SF/A-3/S-1` |
| 9 | Ender's Game | Orson Scott Card | 1985 | SF | `9780812540833` | 2 | `MAIN/3F/SF/A-3/S-2` |
| 10 | Brave New World | Aldous Huxley | 1932 | SF | `9780060850524` | 2 | `MAIN/3F/SF/A-4/S-1` |
| 11 | The Pragmatic Programmer | David Thomas, Andrew Hunt | 1999 | SCI | `9780135957059` | 3 | `MAIN/1F/SCI/A-1/S-2` |
| 12 | Structure and Interpretation of Computer Programs | Harold Abelson, Gerald Jay Sussman | 1985 | SCI | `9780262510875` | 2 | `MAIN/1F/SCI/A-1/S-3` |
| 13 | Gödel, Escher, Bach | Douglas Hofstadter | 1979 | SCI | `9780465026562` | 1 | `MAIN/1F/SCI/A-2/S-1` |
| 14 | Designing Data-Intensive Applications | Martin Kleppmann | 2017 | SCI | `9781449373320` | 3 | `MAIN/1F/SCI/A-2/S-2` |
| 15 | Clean Code | Robert C. Martin | 2008 | SCI | `9780132350884` | 2 | `MAIN/1F/SCI/A-2/S-3` |
| 16 | Introduction to Algorithms | Thomas H. Cormen, Charles E. Leiserson, Ronald L. Rivest, Clifford Stein | 2009 | SCI | `9780262033848` | 1 | `MAIN/1F/SCI/A-3/S-1` |
| 17 | The Elements of Statistical Learning | Trevor Hastie, Robert Tibshirani, Jerome Friedman | 2009 | SCI | `9780387952826` | 1 | `MAIN/1F/SCI/A-3/S-2` |
| 18 | A Brief History of Time | Stephen Hawking | 1988 | SCI | `9780553380163` | 2 | `MAIN/1F/SCI/A-3/S-2` |
| 19 | Thinking, Fast and Slow | Daniel Kahneman | 2011 | SCI | `9780374533557` | 1 | `MAIN/1F/SCI/A-3/S-2` |
| 20 | The C Programming Language | Brian W. Kernighan, Dennis M. Ritchie | 1988 | SCI | `9780131103627` | 2 | `MAIN/1F/SCI/A-3/S-2` |
| 21 | Sapiens: A Brief History of Humankind | Yuval Noah Harari | 2011 | HVA | `9780062316097` | 3 | `MAIN/2F/HVA/A-1/S-2` |
| 22 | Guns, Germs, and Steel | Jared Diamond | 1997 | HVA | `9780393317558` | 1 | `MAIN/2F/HVA/A-1/S-3` |
| 23 | 1491: New Revelations of the Americas Before Columbus | Charles C. Mann | 2005 | HVA | `9781400032051` | 2 | `MAIN/2F/HVA/A-2/S-1` |
| 24 | Postwar: A History of Europe Since 1945 | Tony Judt | 2005 | HVA | `9780143037750` | 1 | `MAIN/2F/HVA/A-2/S-2` |
| 25 | The Guns of August | Barbara W. Tuchman | 1962 | HVA | `9780345476098` | 1 | `MAIN/2F/HVA/A-3/S-1` |
| 26 | Salt: A World History | Mark Kurlansky | 2002 | HVA | `9780142001615` | 1 | `MAIN/2F/HVA/A-3/S-2` |
| 27 | The Story of Art | E. H. Gombrich | 1950 | HVA | `9780714832470` | 1 | `MAIN/2F/HVA/A-3/S-2` |
| 28 | Ways of Seeing | John Berger | 1972 | HVA | `9780140135152` | 1 | `MAIN/2F/HVA/A-3/S-2` |
| 29 | Zen and the Art of Motorcycle Maintenance | Robert M. Pirsig | 1974 | HVA | `9780060589462` | 1 | `MAIN/2F/HVA/A-3/S-2` |
| 30 | The Name of the Rose | Umberto Eco | 1980 | HVA | `9780156001311` | 1 | `MAIN/2F/HVA/A-3/S-2` |
| 31 | The Chicago Manual of Style (17th ed.) | University of Chicago Press | 2017 | REF | `9780226287058` | 1 | `MAIN/2F/REF/A-1/S-1` |
| 32 | Merriam-Webster's Collegiate Dictionary (11th ed.) | Merriam-Webster | 2003 | REF | `9780877798095` | 1 | `MAIN/2F/REF/A-1/S-2` |
| 33 | The Oxford Guide to Library Research | Thomas Mann | 2005 | REF | `9780199931040` | 0 | — |
| 34 | The Elements of Style (4th ed.) | William Strunk Jr., E. B. White | 1999 | REF | `9780205309023` | 0 | — |
| 35 | The Compact Edition of the Oxford English Dictionary | Oxford University Press | 1991 | REF | `9780198612582` | 1 | `MAIN/2F/REF/A-2/S-1` |
| 36 | A Rulebook for Arguments | Anthony Weston | 1987 | REF | `9780872209541` | 1 | `MAIN/2F/REF/A-2/S-1` |
| 37 | The Economist Style Guide (12th ed.) | The Economist | 2018 | PER | `9781781258446` | 1 | `MAIN/1F/PER/A-1/S-1` |
| 38 | Athenaeum Annual Report, Vol. 90 | Athenaeum Press | 1974 | PER | `9788100000021` | 0 | — |
| 39 | Nature: Volume 512 Highlights | Springer Nature | 2014 | PER | `9788100000045` | 0 | — |
| 40 | Quarterly Bulletin of the Athenaeum, 1952–1953 | Athenaeum Press | 1953 | PER | `9788100000106` | 0 | — |

Notes:

- Archive items **#38–40 intentionally have no Open Library records** — they
  demonstrate the metadata fallback ([08 §6](./08-seed-data.md#6-open-library-fallback)) and
  frontend empty-cover states.
- Publishers/years are illustrative; descriptions are editorial mock blurbs of one
  sentence each (deliberately small chunks — RAG indexes 1–2 chunks per book).
- Dewey/LOC fields are plausible demo strings (e.g. Dune `813.54` / `PS3553.I57 D8 1965`).

## 3. Copy Status Ledger

Each `checked_out` copy also carries one open `loans` row (borrower = a reviewer account,
`due_at = borrowed_at + 14d`, future) so the `/library` Loans tab and due dates have data
([08 §4](08-seed-data.md#4-seed-algorithm) step 5c).

- Metadata order: copies are listed C1, C2, C3 within each book.
- **12 `checked_out` copies** — always the first copy (C1) of a multi-copy book, so
  every checked-out title still shows an available sibling in the UI. Every
  other copy is `available`.

| Book # | Title | Checked-out barcode |
| --- | --- | --- |
| 1 | Dune | `ATH-002941-C01` |
| 2 | Neuromancer | `ATH-002942-C01` |
| 5 | Foundation | `ATH-002945-C01` |
| 11 | The Pragmatic Programmer | `ATH-002951-C01` |
| 14 | Designing Data-Intensive Applications | `ATH-002954-C01` |
| 21 | Sapiens: A Brief History of Humankind | `ATH-002961-C01` |
| 3 | Hyperion | `ATH-002943-C01` |
| 4 | The Left Hand of Darkness | `ATH-002944-C01` |
| 7 | The Three-Body Problem | `ATH-002947-C01` |
| 8 | Project Hail Mary | `ATH-002948-C01` |
| 15 | Clean Code | `ATH-002955-C01` |
| 20 | The C Programming Language | `ATH-002960-C01` |

No `missing_lost` seeded in v1 — exercised only through admin UI mutations
([03 §8.3](03-api-spec.md#83-admin-copies)).

## 4. Shelf Inventory (all 40)

Aisle codes are scoped per section (`SF/A-1` ≠ `SCI/A-1`). `row_level`/`height_tier`
power the "Eye Level" annotations; demo capacity per shelf = 15 slots in the
manifest inspector (Stitch's `38/45` figures are flavor mock numbers, not seeded).

| Floor | Section | Aisle | Shelves (`S-n`) |
| --- | --- | --- | --- |
| 1 (Reading Hall) | SCI — `#10B981` | A-1 | S-1, S-2, S-3 |
| 1 | SCI | A-2 | S-1, S-2, S-3 |
| 1 | SCI | A-3 | S-1, S-2 |
| 1 | PER — `#F59E0B` | A-1 | S-1, S-2 |
| 1 | PER | A-2 | S-1, S-2 |
| 2 (West Stacks) | HVA — `#8B5CF6` | A-1 | S-1, S-2, S-3 |
| 2 | HVA | A-2 | S-1, S-2 |
| 2 | HVA | A-3 | S-1, S-2 |
| 2 | REF — `#6B7280` | A-1 | S-1, S-2 |
| 2 | REF | A-2 | S-1 |
| 3 (North Stacks) | SF — `#3B82F6` | A-1 | S-1, S-2, S-3, S-4 |
| 3 | SF | A-2 | S-1, S-2, S-3, S-4 |
| 3 | SF | A-3 | S-1, S-2, S-3 |
| 3 | SF | A-4 | S-1, S-2, S-3 |
| 3 | SF | A-5 | S-1, S-2, S-3, S-4 |

Row counts: 15 aisles · shelves per cabin: SCI 8, PER 4, HVA 7, REF 3, SF 18 = 40 shelves.

## 5. Edge-case Tables

### Zero-copy books (availability = `not_in_building`, banner → waitlist CTA)

| # | Title | Section | Why seeded |
| --- | --- | --- | --- |
| 33 | The Oxford Guide to Library Research | REF | niche manual, never stocked |
| 34 | The Elements of Style (4th ed.) | REF | holds checkout demo |
| 38 | Athenaeum Annual Report, Vol. 90 | PER | archive-only, no Open Library record |
| 39 | Nature: Volume 512 Highlights | PER | periodical archive fallback demo |
| 40 | Quarterly Bulletin of the Athenaeum, 1952–1953 | PER | oldest record + fallback demo |

### Multi-copy books (manifest/wayfinding-rich)

| # | Title | Copies | Distribution |
| --- | --- | --- | --- |
| 1 | Dune | 3 | `ATH-002941-C01` `A-1/S-2` (checked_out) · C2 `A-1/S-2` · C3 `A-1/S-3` |
| 2 | Neuromancer | 3 | C1 `A-1/S-2` (checked_out) · C2 `A-1/S-2` · C3 `A-2/S-1` |
| 5 | Foundation | 3 | C1 (checked_out) · C2/C3 on `A-2/S-1` |
| 6 | Snow Crash | 3 | C1 `A-2/S-2`, C2 `A-2/S-2`, C3 `A-2/S-3` |
| 11 | The Pragmatic Programmer | 3 | C1 (checked_out) · C2 `SCI/A-1/S-2`, C3 `SCI/A-1/S-3` |
| 14 | Designing Data-Intensive Applications | 3 | C1 (checked_out) · C2/C3 `SCI/A-2/S-2` |
| 21 | Sapiens | 3 | C1 (checked_out) · C2/C3 `HVA/A-1/S-2` |
| 3 | Hyperion | 2 | C1 (checked_out) · C2 `SF/A-1/S-3` |
| 4 | The Left Hand of Darkness | 2 | C1 (checked_out) · C2 `SF/A-2/S-1` |
| 7 | The Three-Body Problem | 2 | C1 (checked_out) · C2 `SF/A-2/S-3` |
| 8 | Project Hail Mary | 2 | C1 (checked_out) · C2 `SF/A-3/S-1` |
| 9 | Ender's Game | 2 | C1/C2 on `SF/A-3/S-2` |
| 10 | Brave New World | 2 | C1/C2 on `SF/A-4/S-1` |
| 12 | SICP | 2 | C1/C2 on `SCI/A-1/S-3` |
| 15 | Clean Code | 2 | C1 (checked_out) · C2 `SCI/A-2/S-3` |
| 16 | Introduction to Algorithms | 1 | C1 on `SCI/A-3/S-1` |
| 17 | The Elements of Statistical Learning | 1 | C1 on `SCI/A-3/S-2` |
| 18 | A Brief History of Time | 2 | C1/C2 on `SCI/A-3/S-2` |
| 19 | Thinking, Fast and Slow | 1 | C1 on `SCI/A-3/S-2` |
| 20 | The C Programming Language | 2 | C1 (checked_out) · C2 `SCI/A-3/S-2` |
| 22 | Guns, Germs, and Steel | 1 | C1 on `HVA/A-1/S-3` |
| 23 | 1491 | 2 | C1/C2 on `HVA/A-2/S-1` |
| 24–30 | (humanities singletons) | 1 each | own rows in the catalog's home shelves |
| 31 | The Chicago Manual of Style | 1 | C1 on `REF/A-1/S-1` |
| 32 | Merriam-Webster's Collegiate Dictionary | 1 | C1 on `REF/A-1/S-2` |
| 35 | The Compact Edition of the OED | 1 | C1 on `REF/A-2/S-1` |
| 36 | A Rulebook for Arguments | 1 | C1 on `REF/A-2/S-1` |
| 37 | The Economist Style Guide | 1 | C1 on `PER/A-1/S-1` |

Copy audit math: 3×7 = 21, 2×11 = 22, 1×17 = 17 → **60 copies** across 35 books;
12 checked out exactly ([§1](#1-fixture-totals)).

### Demo expectations (page-by-page)

- `/books/{Dune}` (book #1): manifest shows 3 copies (1 checked out, 2 available);
  available beacon + "held at Shelf S-2 — Row 4, Eye Level" ladder.
- `/books/{#33}`: `not_in_building` — waitlist CTA path with
  `suggested_actions: ["reserve","notify_when_available"]`.
- `/books/{#34}`, `{#38}`, `{#39}`, `{#40}`: zero-copy display + waitlist demo
  (with history rows).
- `/catalog?availability=available`: 35 (books w/ ≥1 available copy).
- `/catalog?section=SF`: 10 results · `/catalog?mode=keyword&q=dune`: Dune #1 first.
- Hybrid mode: topical queries (e.g. "what can I read about memory and data systems?")
  hit DDIA, SICP, ESL, CLRS in different order than keyword.

## 6. Demo Users & Personal Library

| Login | Role | Password |
| --- | --- | --- |
| `admin@athenaeum.dev` | admin | `athenaeum-demo` |
| `demo@athenaeum.dev` | user | `athenaeum-demo` |
| `user1@gmail.com` … `user20@gmail.com` | user | `user1` … `user20` (local part) |

The 20 reviewer accounts (decided 2026-10-16) exist to give every book a **rating average**
([02 §6.2](02-data-model.md#62-book_ratings)) and to own the seeded loans. They are ordinary
login-able users (you can sign in as `user1@gmail.com` / `user1`); their ratings are
deterministic (hash of user+book, values 3-5) so the catalog shows varied `★ x.y (n)` and some
books stay unrated. Never used by the e2e suite.

`demo`'s seeded `user_library` (24 entries — matches the `/library` KPI cards):

| Stage | Books (#) |
| --- | --- |
| `reading` (2) | Dune (progress 40%) · Project Hail Mary (progress 12%) |
| `read` (15) | Neuromancer, Foundation, Snow Crash, Hyperion, Three-Body, Clean Code, SICP, C Programming, Brief History, 1491, Guns of August, Salt, Ways of Seeing, Name of the Rose, Ender's Game |
| `want_to_read` (7) | Left Hand of Darkness, GEB, Thinking Fast, Story of Art, Postwar, Guns Germs & Steel, Designing Data-Intensive Applications |
| favorites (7) | Dune, Neuromancer, Foundation, Snow Crash, Hyperion, Three-Body, Designing Data-Intensive Applications |
| ratings seeded | Neuromancer 5, Foundation 4, Snow Crash 5, SICP 5, 1491 4 |

> Correction: the favorites list includes *Designing Data-Intensive Applications*, which must be a library row to be favorited; it replaces *Zen and the Art of Motorcycle Maintenance* in `want_to_read` so the 24-entry / 7-favorite totals hold. Seeded from `api/mocks/library.json` (titles resolve against `books.json`; the account follows `SEED_USER_EMAIL`).

The reading-stage enum now includes **`dropped`** ([02 §4](02-data-model.md#user_library)); the demo
library seeds none, so the Dropped tab shows an empty state.

`waitlist` seeds (2): demo holds on #33 and #40 (`status='waiting'`, `position`
1 each — no other users so queues are short).

`admin` starts with empty collection and 0 waitlist rows (clean admin screens).

## 7. Extending the Mock Data

1. Append (or edit) entries in the schemas of [08 §2-3](08-seed-data.md#2-mocksbuildingjson-schema);
   keep natural-key discipline (`section_code` must exist; `shelf_path` resolvable;
   ISBN-13 must pass checkdigit; `copies[]` statuses from the allowed set).
2. When a fixture is added/emended: re-run `make seed` — upserts keep
   everything idempotent; `--reset` wipes and rebuilds content first.
3. For new Open Library-bound books, keep description short; reindex
   (`make reindex`) refreshes embeddings ([06 §2](06-ai-rag.md#2-rag-pipeline)).
4. `docs/screenshots` — none required by fixtures; UI screenshots live in
   `.artifacts/stitch/` ([12 §7](12-ui-design.md#7-stitch-screen-gallery)).
