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
| UI strings for screens 1–5 (`/login`, `/catalog`, `/books/[id]`, `/library`, `/chat`) and the app shell | Admin screens 6a/6b — stay English |
| zh display-label maps for API enums (availability, copy status, reading stage, badges) | Translating catalog records: book titles, authors, descriptions, section names, and the [13](./13-mock-data-reference.md) mock contract stay English |
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
- Locale changes go through a server action (`setLocale`) that writes the cookie and
  refreshes server components — see §9.
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
  `chat.composer.placeholder`, …) so screens stay greppable.
- No user-facing literals in components — every string resolves through the catalog.
- ICU messages cover interpolation and plurals; the en and zh-CN catalogs MUST stay
  key-for-key in sync (a unit test enforces parity — §12).

## 6. Enum Display-Label Maps

The API keeps returning **English enum values** ([03](./03-api-spec.md)); the web layer
maps them to localized labels. No backend or data change.

| Enum | Source | Examples |
| --- | --- | --- |
| Availability state | `GET /api/books/*`, search | `available`, `in_building`, `checked_out`, `missing_lost` |
| Copy status | admin / detail copy manifest | `available` / `checked_out` / …, [12 §10](./12-ui-design.md#10-copy-state-machine) |
| Reading stage | `GET /api/library?status=` | `reading`, `read`, `want_to_read`, `favorites` |
| Availability/copy beacons | [12 §3](./12-ui-design.md#3-design-tokens) | badge and Alert labels |

> The mapping table lives beside the component primitives so a new enum value is a
> single-place edit; unknown values fall back to the raw English string.

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
- Behavior: posts to the `setLocale` server action, which writes `athenaeum_lang` and
  refreshes server components. No full navigation, no URL change.

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
| E2E | Playwright: toggle switches and persists across navigation, cookie is set, no literal English leaks ([15 §9](./15-voice-input.md#9-testing) shares the suite) |

## 13. Traceability

| Concern | Owner |
| --- | --- |
| Route list / app shell | [07-frontend.md](./07-frontend.md), [12-ui-design.md](./12-ui-design.md#6-app-shell--navigation) |
| Chat request/SSE contract | [03-api-spec.md](./03-api-spec.md#71-sse-stream), [06-ai-rag.md](./06-ai-rag.md#6-sse-event-contract) |
| Env vars / topology | [01-architecture.md](./01-architecture.md#7-environment-variables) |
| Scope / deferrals | [10-roadmap.md](./10-roadmap.md) |
| Dev workflow + E2E | [09-development.md](./09-development.md#8-testing-strategy) |
