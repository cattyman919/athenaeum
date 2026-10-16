// Patron Lookup helpers (docs/11 §7–§8, docs/03 §5.4/§6.4): API error → UX view mapping and
// result updates after a self-service return. Pure functions only — unit-tested in node.

import { ApiError } from "./api-client";
import { rateLimitSeconds } from "./api-errors";
import type { ReturnResponse, ScanResponse } from "./types";

export type ScanErrorView =
  | { kind: "invalid_isbn" }
  | { kind: "rate_limited"; seconds: number | null }
  | { kind: "not_found" }
  | { kind: "conflict"; currentStatus: string | null }
  | { kind: "generic" };

/** Maps a failed scan/return call onto the docs/11 §8 error table. */
export function scanErrorView(err: unknown, nowMs = Date.now()): ScanErrorView {
  if (!(err instanceof ApiError)) return { kind: "generic" };
  if (err.status === 429)
    return { kind: "rate_limited", seconds: rateLimitSeconds(err.headers, nowMs) };
  if (err.status === 400 && err.code === "INVALID_ISBN") return { kind: "invalid_isbn" };
  if (err.status === 404) return { kind: "not_found" };
  if (err.status === 409) {
    const cs = err.body?.current_status;
    return { kind: "conflict", currentStatus: typeof cs === "string" ? cs : null };
  }
  return { kind: "generic" };
}

/** Result after a successful return: the copy's new state + refreshed availability. */
export function applyReturn(result: ScanResponse, ret: ReturnResponse): ScanResponse {
  return {
    ...result,
    copy: ret.copy,
    availability: ret.availability,
    wayfinding: ret.copy.location ?? result.wayfinding,
    returnable: ret.copy.status === "checked_out",
  };
}

/** Result after a 409 CONFLICT: reflect the copy's real status (docs/11 §8). */
export function applyConflict(result: ScanResponse, currentStatus: string | null): ScanResponse {
  if (!result.copy || !currentStatus) return { ...result, returnable: false };
  return {
    ...result,
    copy: { ...result.copy, status: currentStatus },
    returnable: currentStatus === "checked_out",
  };
}

/** True when the waitlist CTA replaces "copies left" (docs/07 §Scanner dialog). */
export function needsWaitlist(result: ScanResponse): boolean {
  return result.match === "book" && (result.availability?.available_copies ?? 0) === 0;
}
