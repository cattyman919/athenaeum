# Localization (English / Simplified Chinese)

> The EN/zh-CN localization contract for the patron-facing app: supported locale tags, the `athenaeum_lang` cookie, `Accept-Language` default resolution, the next-intl setup, message-catalog layout, enum display-label maps, CJK typography, the top-nav language toggle, and the AI reply-language directive — feature framing in [10-roadmap.md](./10-roadmap.md#1-in-scope-v1--checklist), AI specifics in [06-ai-rag.md](./06-ai-rag.md), page wiring in [07-frontend.md](./07-frontend.md), visuals in [12-ui-design.md](./12-ui-design.md).

## Contents

1. [Scope](#1-scope)
2. [Locale Model](#2-locale-model)
3. [Storage & Routing](#3-storage--routing)
4. [Implementation (next-intl)](#4-implementation-next-intl)
5. [Message Catalog](#5-message-catalog)
6. [Enum Display-Label Maps](#6-enum-display-label-maps)
7. [Formatting](#7-formatting)
8. [Typography & Fonts](#8-typography--fonts)
9. [Language Toggle](#9-language-toggle)
10. [AI Reply Language](#10-ai-reply-language)
11. [Known Limitations](#11-known-limitations)
12. [Testing](#12-testing)
13. [Traceability](#13-traceability)

## 1. Scope

| In scope | Out of scope |
| --- | --- |
| UI strings for screens 1–5 (`/login`, `/catalog`, `/books/[id]`, `/library`, `/chat`), the app shell, and the **Barcode Scanner dialog's Patron Lookup** (nav Scan button, `/library` borrow/return) | Admin screens 6a/6b **and the scanner's Staff Intake tab** — stay English |
| zh display-label maps for API enums (availability, copy status, reading stage, badges) | Translating catalog records: book titles, authors, descriptions, and the [13](./13-mock-data-reference.md) mock contract stay English |
| **Wayfinding text** — the turn-by-turn ladder, copy-manifest locations, library shelf coordinates, citation shelf lines, section chips/facets (floor names, section names, height tiers; [§6.1](#61-wayfinding-vocabulary)) | Machine tokens: location path `MAIN/3F/SF/A-1/S-2`, barcodes, aisle/shelf codes, Dewey/LOC call numbers — always as stored |
| AI Librarian answers in the selected language | Spoken/voice output (input only — [15](./15-voice-input.md)) |
| `en` and `zh-CN` only | `zh-TW`/`zh-HK` or any third locale |

> ⚠️ TODO: Traditional Chinese (`zh-TW`) and any additional locale are deferred; revisit
> in [10 §5](./10-roadmap.md#5-future-enhancements).

## 2. Locale Model

- Supported tags: **`en`** and **`zh-CN`** (Simplified Mandarin). These are the only
  accepted values for the cookie and for the chat `locale` field.
- Any other value is treated as unset and falls back to the resolved default.
- The `<html lang>` attribute is set from the active locale (`en` / `zh-CN`).

## 3. Storage & Routing

The locale lives in a cookie and there is **no URL path prefix** — every existing route
and deep link ([07 §3](./07-frontend.md#3-stitch-route-mapping)) is language-agnostic.

| Concern | Decision |
| --- | --- |
| Cookie name | `athenaeum_lang` |
| Attributes | `Path=/`, `SameSite=Lax`; not `HttpOnly` (the toggle may read it client-side); server components read the same cookie |
| Resolution order | cookie → `Accept-Language` header → `en` |
| Persistence | first successful resolution is written back to the cookie so the choice is stable |
| Shareability | language is **not** shareable via link (intentional — cookie, not URL) |
| Middleware | [07 §2](./07-frontend.md#2-page-map) route guard is unchanged; locale resolution happens in the app shell / request config, not the auth guard |

## 4. Implementation (next-intl)

- Library: **next-intl**, configured **without i18n routing** (no `[locale]` segment).
- The request config resolves the locale from the cookie/`Accept-Language` per §3 and
  loads the matching catalog (§5).
- Server components read messages directly; client components use `useTranslations`.
- Locale changes write the (non-httpOnly) `athenaeum_lang` cookie in the browser and hard-reload
  the page — see §9.
- `app/layout.tsx` sets `<html lang={locale}>` and provides the messages.

```text
web/
├── i18n/request.ts        # resolve locale (cookie → Accept-Language → en) + load messages
├── messages/en.json       # typed catalog
├── messages/zh-CN.json    # typed catalog
└── components/language-toggle.tsx
```

## 5. Message Catalog

- One JSON catalog per locale at `web/messages/{locale}.json`.
- **Keys are namespaced by area** (`shell.nav.catalog`, `catalog.filters.available`,
  `chat.composer.placeholder`, `scanner.patron.*`, …) so screens stay greppable.
- `catalog.indexingNote` (ICU plural, en + zh-CN) is the patron note shown while books are still being indexed ([07](07-frontend.md)); the admin index badges/chip stay English like the rest of admin.
- New patron keys for these features (en + zh-CN parity, §12): the rating average on cards/detail (`catalog.card.rating` = `★ {avg} ({count})`, `book.rating.none`), the catalog **Advanced** mode reveal, the `/library` **Loans** tab (`library.tabs.loans`, `library.loans.{due,dueSoon,overdue,renew,renewed,returned}`), and the scanner borrow/return actions (`scanner.patron.{borrow,returned,dueOn,borrowed}`). API enum labels for loans (`active`/`past`) map through the enum table (§6).
- No user-facing literals in components — every string resolves through the catalog.
- ICU messages cover interpolation and plurals; the en and zh-CN catalogs MUST stay
  key-for-key in sync (a unit test enforces parity — §12).

## 6. Enum Display-Label Maps

The API keeps returning **English enum values** ([03](./03-api-spec.md)); the web layer
maps them to localized labels. No data change; the only API addition is the structured
wayfinding `parts` object ([§6.1](#61-wayfinding-vocabulary)).

| Enum | Source | Examples |
| --- | --- | --- |
| Availability state | `GET /api/books/*`, search | `available`, `in_building`, `checked_out`, `missing_lost` |
| Copy status | admin / detail copy manifest | `available` / `checked_out` / …, [12 §10](./12-ui-design.md#10-copy-state-machine) |
| Reading stage | `GET /api/library?status=` | `reading`, `read`, `want_to_read`, `favorites` |
| Availability/copy beacons | [12 §3](./12-ui-design.md#3-design-tokens) | badge and Alert labels |

> The mapping table lives beside the component primitives so a new enum value is a
> single-place edit; unknown values fall back to the raw English string.

### 6.1 Wayfinding vocabulary

Wayfinding prose is a **presentation concern**: the web composes it from structured parts and the
catalog, so the ladder reads naturally in the active locale. The API keeps English prose
(`wayfinding.ladder_steps`, `copies[].location.ladder`, `section.name`) as the fallback used by
admin screens, tool results and logs — the web **does not render those strings** on screens 1–5.

Structured parts (additive field `wayfinding.parts` and `copies[].location.parts`,
[03 §5.2](03-api-spec.md#52-get-apibooksid)): `building_code`, `building_name`, `floor_number`,
`floor_name`, `section_code`, `section_name`, `aisle_code`, `shelf_code`, `row_level`, `height_tier`.

Catalog keys (both `messages/en.json` and `messages/zh-CN.json`, parity-tested):

| Namespace | Keyed by | en → zh-CN |
| --- | --- | --- |
| `wayfinding.building.<code>` | building code | `MAIN` Athenaeum Main Building → 雅典娜图书馆主楼 |
| `wayfinding.floor.<n>` | `floor_number` | 1 Reading Hall → 阅览大厅 · 2 West Stacks → 西区书库 · 3 North Stacks → 北区书库 |
| `wayfinding.section.<code>` | section code | `SCI` Science, Mathematics & Tech → 科学、数学与技术 · `PER` Periodicals & Archives → 期刊与档案 · `HVA` History, Humanities & Arts → 历史、人文与艺术 · `REF` Reference & Special Folios → 参考与特藏 · `SF` Science Fiction & Speculative → 科幻与奇幻 |
| `wayfinding.tier.<slug>` | `height_tier` slug (`low_stacks`, `mid_stacks`, `eye_level`, `tower_top`) | Low Stacks → 底层书架 · Mid Stacks → 中层书架 · Eye Level → 平视高度 · Tower Top → 顶层书架 |
| `wayfinding.step.*` | ICU templates | `floor`: "Floor {n} — {name}" → "{n} 楼 — {name}" · `section`: "Section: {name}" → "分区：{name}" · `aisle`: "Aisle {code}" → "{code} 通道" · `shelf`: "Shelf {code} — Row {row}, {tier}" → "书架 {code} — 第 {row} 层，{tier}" |
| `wayfinding.shelfLine` | ICU template | "Floor {n} • Shelf {code}" → "{n} 楼 • 书架 {code}" (library rows, citation cards) |

Rules:

- Codes (`A-2`, `S-2`), the path token, barcodes and call numbers are **never translated**; they sit
  inside the translated sentence unchanged.
- Lookup falls back to the raw English value from the API when a key is missing, so floors, sections
  or tiers an admin adds after seeding degrade to English until added to the catalogs.
- Applies to: `BookLocationCard` ladder, copy-manifest location column, catalog facet/section chips
  and the book-page section chip (names looked up by `section_code`), `/library` rows, and `/chat`
  `AICitationCard` shelf lines. The admin screens stay English ([§1](#1-scope)).
- The AI reply language is separate ([§10](#10-ai-reply-language)); when the model quotes a location
  in prose it follows the reply language, while citation cards use this vocabulary.

## 7. Formatting

- Dates, numbers, and relative times are formatted through next-intl formatters.
- The **timezone stays `America/New_York`** (the library building — [01 §8](./01-architecture.md#8-cross-cutting-rules));
  localization changes presentation, not the timezone.

## 8. Typography & Fonts

- `Inter` (UI) and `JetBrains Mono` (call numbers, barcodes, telemetry) are unchanged
  ([12 §4](./12-ui-design.md#4-typography)).
- **Inter has no CJK glyphs**, so a CJK-capable fallback is added to the stack — default
  **Noto Sans SC** via `next/font` — and recorded with the tokens in
  [12](./12-ui-design.md#3-design-tokens). Without it, zh text renders in an OS fallback.
- Mono (`JetBrains Mono`) is used only for machine strings (barcodes, call numbers,
  shelf coordinates); zh prose uses the UI stack.

## 9. Language Toggle

- Placement: the authenticated top nav ([12 §6](./12-ui-design.md#6-app-shell--navigation)),
  and also on `/login` so the choice is reachable before auth.
- Control: a two-state toggle (`EN` ⇄ `中文`) with `aria-label` and `aria-pressed`, keyboard
  operable ([12 §12](./12-ui-design.md#12-accessibility-requirements)).
- Behavior: writes `athenaeum_lang` client-side and reloads the current page (no URL change).
  A server action + soft refresh was dropped: it lost a race with in-flight link prefetches and
  intermittently left the page in the old language.

## 10. AI Reply Language

The AI answers in the active locale (decision in [10 §1](./10-roadmap.md#1-in-scope-v1--checklist)).

| Concern | Contract |
| --- | --- |
| Request | optional `"locale": "en" \| "zh-CN"` added to the `POST /api/chat` body ([03 §7.1](./03-api-spec.md#71-sse-stream)); omitted → `en` |
| Validation | unknown/missing locale → `en` |
| Prompt | the service appends a locale directive to the system prompt beside the existing rails ([06 §8](./06-ai-rag.md#8-guardrails)): answer in English / Simplified Chinese; still cite only catalog shelf paths; still call `search_catalog` |
| SSE | the `meta` event echoes `locale` ([06 §6](./06-ai-rag.md#6-sse-event-contract)) — additive, event order unchanged |
| Persistence | no schema change; historical turns replay their stored text, so reopening an EN thread under a ZH toggle shows the original EN replies (expected) |
| Whisper hint | the same locale maps to the transcription `language` hint (`zh`/`en`) — [15 §4](./15-voice-input.md#4-transcription-endpoint) |

## 11. Known Limitations

- **Chinese semantic retrieval** degrades: the embedding model (`nomic-embed-text`) is
  English-only, so the pgvector pre-retrieval ([06 §2](./06-ai-rag.md#2-rag-pipeline))
  may return little for a zh query. The Eino agent is an LLM and can still form English
  `search_catalog` queries when answering in Chinese, so tool-based retrieval works.

> ⚠️ TODO: multilingual embeddings or query translation for zh semantic/hybrid search —
> see [06 §2](./06-ai-rag.md#2-rag-pipeline) and [10 §5](./10-roadmap.md#5-future-enhancements).

## 12. Testing

| Layer | Coverage |
| --- | --- |
| Locale resolution | unit: cookie > `Accept-Language` > `en`; invalid value → `en` |
| Catalog parity | unit: `en.json` and `zh-CN.json` have identical key sets |
| Enum maps | unit: every enum value used in the UI has a label; unknown falls back |
| Wayfinding vocabulary | unit: every building/floor/section/height tier in [13](13-mock-data-reference.md) has an `en` and `zh-CN` entry; ICU placeholders match; unknown values fall back to the raw API string |
| E2E | Playwright: toggle switches and persists across navigation, cookie is set, no literal English leaks — including the wayfinding ladder, which must read in Chinese while the `MAIN/…` path token stays unchanged ([15 §9](./15-voice-input.md#9-testing) shares the suite) |

## 13. Traceability

| Concern | Owner |
| --- | --- |
| Route list / app shell | [07-frontend.md](./07-frontend.md), [12-ui-design.md](./12-ui-design.md#6-app-shell--navigation) |
| Chat request/SSE contract | [03-api-spec.md](./03-api-spec.md#71-sse-stream), [06-ai-rag.md](./06-ai-rag.md#6-sse-event-contract) |
| Env vars / topology | [01-architecture.md](./01-architecture.md#7-environment-variables) |
| Scope / deferrals | [10-roadmap.md](./10-roadmap.md) |
| Dev workflow + E2E | [09-development.md](./09-development.md#8-testing-strategy) |
