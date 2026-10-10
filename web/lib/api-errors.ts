// Error → toast text for patron mutations (docs/07 §9): a 429 reports the remaining time from
// `X-RateLimit-Reset` (epoch seconds, docs/03 §2) or `Retry-After`; anything else falls back
// to the caller's generic message.

import { ApiError } from "./api-client";

export interface ErrorTranslator {
  (key: string, values?: Record<string, string | number>): string;
}

/** Seconds until the rate-limit window resets; null when the headers are absent/invalid. */
export function rateLimitSeconds(headers: Headers | undefined, nowMs = Date.now()): number | null {
  if (!headers) return null;
  const retry = headers.get("Retry-After");
  if (retry && /^\d+$/.test(retry.trim())) return Math.max(1, Number(retry.trim()));
  const reset = headers.get("X-RateLimit-Reset");
  if (reset && /^\d+$/.test(reset.trim())) {
    return Math.max(1, Math.ceil(Number(reset.trim()) - nowMs / 1000));
  }
  return null;
}

/**
 * Toast text for a failed request. `t` is scoped to the `common` catalog
 * (`errors.rateLimited`, `errors.rateLimitedSoon`).
 */
export function apiErrorText(
  t: ErrorTranslator,
  err: unknown,
  fallback: string,
  nowMs = Date.now(),
): string {
  if (err instanceof ApiError && err.status === 429) {
    const seconds = rateLimitSeconds(err.headers, nowMs);
    return seconds === null ? t("errors.rateLimitedSoon") : t("errors.rateLimited", { seconds });
  }
  return fallback;
}
