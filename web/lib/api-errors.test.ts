import { describe, expect, it } from "vitest";
import { ApiError } from "./api-client";
import { apiErrorText, rateLimitSeconds } from "./api-errors";

const t = (key: string, values?: Record<string, string | number>) =>
  values ? `${key}:${JSON.stringify(values)}` : key;

describe("rate-limit toast text (docs/07 §9)", () => {
  const now = 1_000_000_000_000; // ms

  it("reads the remaining time from X-RateLimit-Reset (epoch seconds)", () => {
    const h = new Headers({ "X-RateLimit-Reset": String(now / 1000 + 42) });
    expect(rateLimitSeconds(h, now)).toBe(42);
  });

  it("prefers Retry-After and never reports less than one second", () => {
    expect(rateLimitSeconds(new Headers({ "Retry-After": "7" }), now)).toBe(7);
    expect(
      rateLimitSeconds(new Headers({ "X-RateLimit-Reset": String(now / 1000 - 5) }), now),
    ).toBe(1);
    expect(rateLimitSeconds(new Headers(), now)).toBeNull();
    expect(rateLimitSeconds(undefined, now)).toBeNull();
  });

  it("formats 429s and falls back for other errors", () => {
    const limited = new ApiError(
      429,
      "RATE_LIMITED",
      "slow down",
      undefined,
      new Headers({ "X-RateLimit-Reset": String(now / 1000 + 30) }),
    );
    expect(apiErrorText(t, limited, "fallback", now)).toBe('errors.rateLimited:{"seconds":30}');
    expect(apiErrorText(t, new ApiError(429, "RATE_LIMITED", "x"), "fallback", now)).toBe(
      "errors.rateLimitedSoon",
    );
    expect(apiErrorText(t, new ApiError(500, "INTERNAL", "x"), "fallback", now)).toBe("fallback");
    expect(apiErrorText(t, new Error("boom"), "fallback", now)).toBe("fallback");
  });
});
