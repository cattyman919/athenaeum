# Plan: Voice Input + EN/ZH Localization + Git Workflow + Playwright E2E (v1 scope addition)

## Goal

Add four capabilities to the Athenaeum build (currently docs-only; Phase 0 done, Phase 1
not started):

1. **Voice input on `/chat`** — push-to-talk recording transcribed by a self-hosted
   Whisper service, inserted into the composer as editable text.
2. **EN/zh-CN localization** — the whole patron-facing app (screens 1–5) renders
   localized UI strings, and the AI Librarian replies in the selected language.
3. **A git commit workflow** — Conventional Commits, one commit per feature/bug,
   enforced by hooks, documented in doc 09 and referenced from `AGENTS.md`.
4. **Playwright E2E + CI** — an end-to-end suite covering route smoke + key journeys,
   run by a new GitHub Actions pipeline.

None of these exist in any doc today, so this plan is an **explicit v1 scope
change**. `AGENTS.md` forbids silent scope additions — the owning docs below MUST be
edited alongside the code.

## Locked decisions

| # | Decision | Choice |
| --- | --- | --- |
| 1 | Localization surface | Whole patron app (screens 1–5) UI strings **+ AI replies in selected language**. Admin (screen 6) stays English. |
| 2 | Locale storage | `athenaeum_lang` cookie, read by server components. **No URL prefix**; existing routes/deep-links unchanged. |
| 3 | i18n library | **next-intl**, configured without i18n routing; locale sourced from the cookie. |
| 4 | Locale variants | `en` and `zh-CN` (Simplified) only. Traditional deferred. |
| 5 | AI language signal | Optional `locale` field in `POST /api/chat` body; echoed in the `meta` SSE event; injected into the system prompt. No DB/schema change. |
| 6 | Chinese RAG | Accept degradation of English `nomic-embed-text` retrieval for zh queries; rely on the LLM agent to form English `search_catalog` queries. Document as a known limitation + `> ⚠️ TODO:`. |
| 7 | STT engine | Self-hosted **faster-whisper `small`** container, behind a Go `WhisperAdapter` (ACL, mirrors Ollama). |
| 8 | Voice UX | Push-to-talk → transcript into composer → user presses Send. Not auto-send, not always-on. |
| 9 | Transcribe contract | Auth required; 60s / ~10MB cap; 30s upstream timeout; dedicated 10/min per-user bucket; `503 TRANSCRIPTION_UNAVAILABLE` when down; silent clip → `200 {text:""}`. |
| 10 | Control placement | Language toggle in the top nav (12 §6); mic toggle in the chat composer. |
| 11 | Stitch | Add a "Localization & Voice" subsection to doc 12; best-effort regenerate the chat screen via Stitch MCP; if unstable, record the deviation in 12 (do not block). |
| 12 | Roadmap | Fold into v1: new rows in 10 §1 (localization, voice, git workflow, E2E/CI) + new TODO phase(s). |
| 13 | Default language | First visit → derive from `Accept-Language`, fall back to `en`; persist cookie; cookie always wins thereafter. Toggle available on `/login`. |
| 14 | Translation depth | UI chrome + zh display-label maps for API enums (availability, copy status, reading stage, badges). Book titles/authors/descriptions and doc 13 mock values stay English. |
| 15 | Unit validation | Go + web unit tests for new logic; manual bilingual QA checklist. |
| 16 | Compose | Whisper is an **always-on** service beside `postgres`/`redis`/`ollama`, with a documented one-time model pull. |
| 17 | Commit convention | **Conventional Commits**, fixed types (feat, fix, docs, test, refactor, perf, chore, build, ci) + fixed scopes (api, web, db, infra, docs, chat, voice, i18n). Body explains why; footer cites the TODO/issue. |
| 18 | Hook enforcement | **lefthook + commitlint**: `pre-commit` (format/lint staged files) + `commit-msg` (validate subject). Fills the dangling "hooks below" gap in 09 §1. |
| 19 | Commit granularity | **One short-lived branch per TODO task → squash-merge** to main, so main holds exactly one Conventional Commit per feature/bug. Commit only when code + tests + docs are green. |
| 20 | E2E scope | Route smoke for all six routes + key journeys (login guard, three search modes, 409 waitlist, library tabs, chat SSE turn, admin role gate, i18n toggle, voice composer). |
| 21 | E2E environment | Real compose stack + seeded DB; `OPENROUTER_BASE_URL` → local fake ChatModel, `WHISPER_BASE_URL` → stub, `OFFLINE_MODE=true`. |
| 22 | E2E harness | `storageState` auth reuse (admin + patron), **Chromium only** in v1, specs in `web/e2e/`, `make e2e` target. |
| 23 | CI | New **GitHub Actions** workflow: lint + vet + Go unit + web unit + Playwright E2E. Reverses the 09 §8 CI deferral. |
| 24 | Doc structure | Two **new docs** — `14-localization.md` + `15-voice-input.md` (mirrors `11-barcode-scanning.md`) — each owning its full contract; git workflow + E2E live in `09-development.md`. Update `docs/README.md` §4 map + `AGENTS.md` doc table. |

## Non-goals (explicitly out of scope)

- Text-to-speech / spoken AI answers (input only).
- Traditional Chinese (`zh-TW`/`zh-HK`), any locale beyond `en` + `zh-CN`.
- Translating catalog records or the doc 13 mock contract.
- Changing the SSE event **order** or the `conversations`/`messages` schema.
- Admin-facing localization.
- Multilingual embeddings or query translation (documented TODO only).
- Cross-browser / mobile-viewport E2E (Chromium desktop only in v1; later axis).
- Changelog/release automation on top of Conventional Commits (convention enables it,
  but generating changelogs is not in this plan).

## Source facts (verified)

- `web/` and `api/` contain no code yet — this is greenfield; retrofitting later is
  more expensive than building i18n in from the first screen component.
- Chat request body contract: `docs/03-api-spec.md` §7.1.
- SSE events / agent loop / guardrails: `docs/06-ai-rag.md` §3, §6, §8.
- Env var table + compose topology: `docs/01-architecture.md` §7; `docs/09-development.md` §2.
- App shell + design tokens: `docs/12-ui-design.md` §6.
- v1 checklist / deferrals: `docs/10-roadmap.md` §1, §5.
- PII rail (audio/transcripts must not be persisted): `docs/06-ai-rag.md` §8.
- **Doc gaps this plan closes**: 09 §1 promises "hooks below" but specifies none; 09 §8
  declares the CI pipeline a `> ⚠️ TODO:` and defers all web testing; 07 §9 carries a
  `> ⚠️ TODO:` stating Playwright e2e is out of v1. Adding E2E + CI reverses 09 §8 and
  07 §9, so both TODO markers must be updated.
- Git state: repo initialized on `main`, **no commits yet** (TODO §2) — so there is no
  history to retrofit; the convention applies from the first commit.
- Provider swap is env-driven (`OPENROUTER_BASE_URL`, `OPENLIBRARY_BASE_URL`
  "overridable for tests/proxies", 01 §7; ACL in 06 §5) — this is what makes the
  stubbed-provider E2E approach possible without a production code path.

## Required documentation changes (do these with the code, not after)

> Plan-mode note: this plan enumerates the edits; the actual `docs/` + `TODO.md` +
> `AGENTS.md` changes are **non-plan files** and must be made by an implementation-capable
> agent (see Handoff).

### New feature docs (mirror `11-barcode-scanning.md`)

1. **`docs/14-localization.md`** — owns the EN/zh-CN contract: locale tags, the
   `athenaeum_lang` cookie, next-intl config, `Accept-Language` default, message-catalog
   location, enum display-label maps, CJK font, `<html lang>`, and the AI-reply-language
   directive. Cross-linked from 06/07/12.
2. **`docs/15-voice-input.md`** — owns push-to-talk UX, `POST /api/chat/transcribe`, the
   `WhisperAdapter`, model + env vars, the compose service, limits / rate bucket / error
   codes, and the no-persist privacy rail. Cross-linked from 03/06/07/09.

### Existing feature docs (cross-link, do not duplicate)

3. **`docs/03-api-spec.md`** — add `POST /api/chat/transcribe` (§7.2); document the
   optional `locale` field in the `POST /api/chat` request body (§7.1); add its rate
   bucket (§2) and new error codes to §9.
4. **`docs/06-ai-rag.md`** — document the locale directive in the system prompt (§8);
   add `locale` to the `meta` event (§6); add the Chinese-RAG limitation + `> ⚠️ TODO:`
   (§2); note the Whisper ACL adapter in `providers.go` (§5) and the transcribe flow in §1.
5. **`docs/07-frontend.md`** — add next-intl to §1; wire the mic button +
   `use-voice-input` hook into §6/§7; add the transcribe client to `lib/api-client.ts`;
   **update the §9 `> ⚠️ TODO:` that currently defers Playwright e2e**.
6. **`docs/12-ui-design.md`** — new "Localization & Voice" subsection (§6 area): toggle
   placement, mic control, CJK font fallback token; update the app-shell diagram.
7. **`docs/01-architecture.md`** — add `WHISPER_BASE_URL`, `WHISPER_MODEL` to the §7
   config table; add Whisper to the §6 compose topology.
8. **`docs/09-development.md`** —
   a. add the Whisper service + one-time model pull to §2 and the stack table §1;
   b. **NEW "Git Workflow" subsection** (near §6): Conventional Commits rules, the
      type/scope vocabulary, one-task-per-branch + squash-merge, and the lefthook +
      commitlint hook setup that finally satisfies the §1 "hooks below" promise;
   c. rewrite §8 Testing Strategy: add a Playwright E2E row + the stub-provider harness;
      **remove/replace the CI `> ⚠️ TODO:`**;
   d. add `make e2e` and `make ci` to the §5 command table.
9. **`.env.example`** — add `WHISPER_BASE_URL` / `WHISPER_MODEL`.

### Project planning & governance docs

10. **`docs/README.md`** (index/contract — must stay truthful):
    - §2 Tech Stack: add faster-whisper (STT), next-intl (i18n), Playwright, lefthook +
      commitlint, GitHub Actions.
    - §3 Feature Scope: add localization, voice input, git workflow, E2E/CI to the
      in-v1 line.
    - §4 Documentation Map: add rows for `14-localization.md` and `15-voice-input.md`.
    - §5 Monorepo Layout: add `.github/`, `web/e2e/`, `web/messages/`, `web/i18n/`,
      `commitlint.config.js`, `lefthook.yml`, and Whisper in the compose comment.
    - §6 System Overview mermaid: add a Whisper provider node.
    - §7 Quickstart: add the Whisper model pull step.
    - §9 Conventions: point commit conventions at 09.
11. **`docs/10-roadmap.md`** — v1 §1 rows (localization, voice, git workflow, E2E/CI);
    §5 future work (multilingual embeddings, cross-browser E2E).
12. **`TODO.md`** — §13 Phase 10: flip the Playwright + CI items from deferred `> ⚠️ TODO:`
    to in-scope; add phase(s) for localization, voice, git workflow, E2E/CI; §14 registry:
    reclassify the `docs/09 §8` CI and `docs/07 §9` Playwright rows and add rows for the
    new `06 §2` Chinese-RAG TODO + the `12` Stitch regeneration; §15 append a Session Log
    row.
13. **`AGENTS.md`** — add doc-table rows for 14/15 and a Git Workflow + E2E ground rule:
    commit each completed feature/bug as part of the workflow, **without waiting for an
    explicit user request**.

### Non-doc artifact

14. **`.github/workflows/ci.yml`** — the pipeline the docs describe: lint + vet + unit +
    Playwright E2E.

## Implementation tasks (ordered)

### A. Web i18n foundation (build this before screen components)

1. Add `next-intl`; create `web/i18n/request.ts` (or equivalent) that resolves locale
   from the `athenaeum_lang` cookie, else `Accept-Language`, else `en`.
2. Add `web/messages/en.json` and `web/messages/zh-CN.json` (typed catalog). Seed with
   shell/nav/footer strings first; grow per screen.
3. Locale resolution helper + a `setLocale` server action that writes the cookie
   (`SameSite=Lax`, `Path=/`, no `HttpOnly` needed only if client must read it —
   prefer server-only reads + a POST action).
4. Set `<html lang>` from the resolved locale in `app/layout.tsx`.
5. Add a CJK-capable font fallback (e.g. Noto Sans SC via `next/font`) to the stack in
   `tailwind.config.ts` + the design tokens; record it in doc 12. Inter alone has no
   CJK glyphs.
6. `LanguageToggle` in the top nav (and on `/login`); posts the locale, updates cookie,
   refreshes server components.

### B. Localize screens 1–5

7. As each of `/login`, `/catalog`, `/books/[id]`, `/library`, `/chat` is built,
   externalize every string to the message catalog (no literals in components).
8. Add zh display-label maps for API enums: availability states, copy statuses,
   reading stages, badge labels. API keeps returning English values; the web layer maps
   them.
9. Format dates/numbers via next-intl formatters (13 §Cross-cutting says dates render in
   `America/New_York` — keep the timezone, localize the presentation).

### C. Backend chat locale

10. Add optional `locale` to the `POST /api/chat` binder; default `en`; validate to
    `{en, zh-CN}`.
11. Echo `locale` in the `meta` SSE event payload.
12. Append a locale directive to the system prompt (with the existing rails in 06 §8):
    respond in English / Simplified Chinese; still cite only catalog shelf paths; still
    use `search_catalog`.
13. Pass `locale` (as `zh`/`en`) into the transcribe language hint where relevant.

### D. Whisper infrastructure

14. Add the whisper service to `docker-compose.yml` (faster-whisper HTTP server image),
    with a named volume, healthcheck, and `WHISPER_BASE_URL` wiring into `api`.
15. Add `WHISPER_BASE_URL` (default `http://localhost:9000`-style) and `WHISPER_MODEL`
    (default `small`) to config + `.env.example`.
16. Add the one-time model pull command to the 09 §2 startup block.

### E. Backend transcribe endpoint

17. `WhisperAdapter` in `api/internal/service/ai/providers.go` (ACL pattern) that POSTs
    audio to the Whisper service with a `language` hint.
18. `POST /api/chat/transcribe` handler → service: auth, size/duration cap, dedicated
    rate bucket, 30s timeout, decode `{text, language}`. Audio never persisted.
19. Error mapping: `503 TRANSCRIPTION_UNAVAILABLE` (service down/timeout), `413`/`400`
    for oversize/bad audio, `200 {text:""}` for silent input; add codes to doc 03 §9.

### F. Web voice client

20. `hooks/use-voice-input.ts`: `MediaRecorder` capture (webm/opus), permission
    handling, record/stop state, POST to `/api/chat/transcribe`, insert transcript into
    the composer.
21. Composer mic toggle: visible recording indicator; disable Send while recording or
    transcribing; "didn't catch that" affordance on empty transcript; error toast on
    503/429.
22. Feature-detect `MediaRecorder` + `getUserMedia`; hide the mic when unsupported.

### G. Docs + unit tests

23. Author `docs/14-localization.md` and `docs/15-voice-input.md` (owning their full
    contracts), then apply the cross-link edits to 01/03/06/07/09/12 and the planning-doc
    edits to `docs/README.md` / `docs/10-roadmap.md` / `TODO.md` / `AGENTS.md`.
24. Go unit tests: transcribe handler against a fake `WhisperAdapter`; locale→language
    hint mapping; `locale` binder validation.
25. Web unit tests: locale cookie resolution (cookie > Accept-Language > en), message
    catalog key coverage (en/zh-CN parity), enum label maps.
26. Manual bilingual QA: toggle on each screen, `/login` default, cookie persistence,
    push-to-talk happy path, mic-denied path, Whisper-down 503 path.

### H. Git workflow + hooks

27. Add `commitlint.config.js` with the type/scope vocabulary (decision 17).
28. Add `lefthook.yml`: `pre-commit` (gofmt + golangci-lint on staged `api/**`, ESLint
    + Prettier on staged `web/**`) and `commit-msg` (commitlint). Document install
    (`lefthook install`) in 09.
29. Add the doc 09 "Git Workflow" subsection and the `AGENTS.md` ground rule.
30. Make the **first commit** the scaffolding per the convention (`chore: scaffold
    monorepo`) so history starts clean. Thereafter: one branch per TODO task,
    squash-merge, one Conventional Commit per feature/bug.

### I. Playwright E2E harness

31. Add `web/playwright.config.ts` (Chromium project, `baseURL` from env, webServer
    pointing at the compose stack, `web/e2e/` testDir).
32. Add global setup that seeds the DB, boots the stack, and captures `storageState`
    for the seeded admin + demo patron (13 §6).
33. Add the stub providers for the test profile: a local fake ChatModel server
    (scripted `meta`/`token`/`tool_call`/`tool_result`/`citations`/`done` events matching
    06 §6) and a Whisper stub returning fixed text; wire via `OPENROUTER_BASE_URL`,
    `WHISPER_BASE_URL`, and `OFFLINE_MODE=true`.
34. Write specs:
    - route smoke ×6 (auth guard / render);
    - login redirect + guard;
    - three catalog search modes (`keyword`/`semantic`/`hybrid`);
    - book detail + 409 waitlist join;
    - library stage tabs;
    - chat turn streamed to `done` (assert token append + citations render);
    - admin role gate (patron gets FORBIDDEN, admin renders);
    - i18n toggle (EN↔zh-CN persists across navigation; cookie set);
    - voice composer (fake media device flags
      `--use-fake-device-for-media-stream --use-fake-ui-for-media-stream`, transcript
      from the Whisper stub lands in the composer).
35. Add `make e2e` (and `make e2e:ui` optional) to doc 09 §5.

### J. CI pipeline

36. Add `.github/workflows/ci.yml`: jobs for Go (`gofmt`/vet/golangci-lint/`go test`),
    web (ESLint/Prettier/`pnpm test`/`pnpm build`), and Playwright E2E against the
    compose stack with the stub providers. Cache pnpm + Go modules; upload Playwright
    traces on failure.
37. Update doc 09 §8 to describe the pipeline; remove the CI `> ⚠️ TODO:`.

## Data flow

- **UI locale:** request → cookie/Accept-Language → `next-intl` → server + client
  components. Toggle → server action → cookie → refresh.
- **AI answer language:** web reads locale → `locale` in POST /api/chat → service
  appends prompt directive → agent replies in that language → `meta.locale` echoed.
- **Voice:** mic → MediaRecorder blob → `POST /api/chat/transcribe` → `WhisperAdapter`
  → Whisper container → `{text}` → composer → normal `POST /api/chat` send path.

## Dev workflow (new)

- **Commit:** TODO task → branch → code + tests + doc edits → `make lint`/`make test`
  green → Conventional Commit (hooks validate) → squash-merge to `main`.
- **E2E:** `make e2e` boots the compose stack with stub providers, seeds the DB,
  captures `storageState`, runs Chromium specs.
- **CI:** push/PR → GitHub Actions runs lint + vet + unit + E2E; failures upload
  Playwright traces.

## Failure modes

| Case | Behavior |
| --- | --- |
| Whisper container down / timeout | `503 TRANSCRIPTION_UNAVAILABLE`; client toast + retry; typing still works. |
| Mic permission denied | Inline message; mic toggle stays available to retry. |
| Browser lacks MediaRecorder/getUserMedia | Mic control hidden; text-only composer. |
| Clip too long / too large | Client blocks at cap; server `413`/`400` as backstop. |
| Silent/empty audio | `200 {text:""}`; "didn't catch that" prompt. |
| Unknown/missing locale | Falls back to `en`; no crash. |
| Chinese semantic retrieval returns nothing | Agent still answers via English tool queries; document limitation. |
| Reopen old conversation under new locale | Old assistant text stays in its original language (expected). |
| Commit subject violates convention | lefthook `commit-msg` rejects; rewrite the message. |
| E2E stub provider drifts from SSE contract | E2E fails loudly; update the fake to match 06 §6. |
| Playwright media fake unavailable in CI | Voice spec gated/skipped with an explicit reason rather than silently passing. |

## Risks / notes

- **CJK fonts** are not optional — Inter has no Chinese glyphs; the Noto fallback must
  be in place before zh rendering is QA'd.
- **Whisper footprint**: `small` model + image adds notable weight to `docker compose
  up`, lengthening first run.
- **Retrofitting i18n is costly**, which is why this plan externalizes strings from the
  first screen component.
- **`meta` event is additive** (`locale`); do not alter event order (06 §6 is load-bearing).
- **`make lint` / `make test`** do not exist yet (Phase 0 unchecked); this plan must add
  them or state the gate it actually uses.
- **CI weight**: GitHub-hosted runners must not boot the real Ollama/Whisper models —
  the E2E job should use the stub providers (decision 21) and a slim compose profile so
  the stack starts fast and secret-free.
- **Commit cadence**: this project commits each completed feature/bug automatically as
  part of the workflow — it is **not** gated on an explicit user request. `AGENTS.md` and
  doc 09 §9 record this so agents commit rather than deferring.
- **Squash-merge needs a remote or `git merge --squash` locally**; document both in the
  Git Workflow subsection since there is no remote yet.

## Validation

- `make lint` + `make test` (once scaffolded) for Go and web.
- New Go/web unit tests green.
- Manual bilingual QA checklist completed for screens 1–5 and both voice paths.
- `make e2e` passes locally against the stubbed compose stack; CI pipeline green on a
  throwaway branch/PR.
- commitlint rejects a deliberately malformed subject; a compliant task commit
  squash-merges to main as one Conventional Commit.
- `docker compose up -d --build` brings up postgres/redis/ollama/whisper/api/web; a
  transcribed clip round-trips into the composer; zh toggle renders through every
  screen without literal English leakage.

## Handoff

This plan's **documentation changes are being applied in-repo now** (agent switched to
code): `docs/14-localization.md` + `docs/15-voice-input.md` authored, and
`docs/README.md` / `docs/10-roadmap.md` / `TODO.md` / `AGENTS.md` updated. Remaining doc
work: the cross-link edits to `01`/`03`/`06`/`07`/`09`/`12` + `.env.example`, a link/
consistency pass, then a `docs:` commit. **Code remains unimplemented** — switch to an
implementation agent for the Go/Next/Playwright work. Commit each completed feature/bug
per doc 09 §9 as it lands; commits are automatic, not request-gated.

### Doc deltas at a glance

- **New:** `docs/14-localization.md`, `docs/15-voice-input.md` (done), `.github/workflows/ci.yml` (later, with code).
- **Edited feature docs:** `01`, `03`, `06`, `07`, `09`, `12`, `.env.example` (in progress).
- **Edited planning/governance docs:** `docs/README.md`, `docs/10-roadmap.md`, `TODO.md`, `AGENTS.md` (done).
