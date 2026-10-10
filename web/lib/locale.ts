// Locale model (docs/14 §2–3): `en` and `zh-CN` only; cookie → Accept-Language → en.

export const LOCALES = ["en", "zh-CN"] as const;
export type Locale = (typeof LOCALES)[number];
export const DEFAULT_LOCALE: Locale = "en";
export const LOCALE_COOKIE = "athenaeum_lang";
export const LOCALE_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (LOCALES as readonly string[]).includes(value);
}

/** Maps one Accept-Language tag to a supported locale (zh-TW/zh-HK are deferred → no match). */
function matchTag(tag: string): Locale | null {
  const t = tag.trim().toLowerCase();
  if (t === "en" || t.startsWith("en-")) return "en";
  if (t === "zh" || t === "zh-cn" || t === "zh-sg" || t === "zh-hans" || t.startsWith("zh-hans-")) {
    return "zh-CN";
  }
  return null;
}

/** Parses an Accept-Language header and returns the best supported locale, if any. */
export function fromAcceptLanguage(header: string | null | undefined): Locale | null {
  if (!header) return null;
  const ranked = header
    .split(",")
    .map((part, index) => {
      const [tag, ...params] = part.trim().split(";");
      const q = params.map((p) => p.trim()).find((p) => p.startsWith("q="));
      const weight = q ? Number.parseFloat(q.slice(2)) : 1;
      return { tag, weight: Number.isNaN(weight) ? 0 : weight, index };
    })
    .filter((e) => e.tag && e.tag !== "*" && e.weight > 0)
    .sort((a, b) => b.weight - a.weight || a.index - b.index);
  for (const { tag } of ranked) {
    const m = matchTag(tag);
    if (m) return m;
  }
  return null;
}

/** Resolution order: valid cookie → Accept-Language → `en`. Invalid cookie values are ignored. */
export function resolveLocale(input: {
  cookie?: string | null;
  acceptLanguage?: string | null;
}): Locale {
  if (isLocale(input.cookie)) return input.cookie;
  return fromAcceptLanguage(input.acceptLanguage) ?? DEFAULT_LOCALE;
}

/** Maps a UI locale to the Whisper/BCP-47 language hint (docs/14 §10). */
export function whisperLanguage(locale: Locale): "zh" | "en" {
  return locale === "zh-CN" ? "zh" : "en";
}
