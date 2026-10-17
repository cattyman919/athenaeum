// Patron Lookup helpers (docs/11 §7–§8, docs/03 §5.4/§6.4): API error → UX view mapping and
// result updates after a self-service return. Pure functions only — unit-tested in node.

import { ApiError } from "./api-client";
import { rateLimitSeconds } from "./api-errors";
import type {
  BorrowBookResponse,
  IdentifyResponse,
  ReturnBookResponse,
  ReturnResponse,
  ScanResponse,
} from "./types";

export type ScanErrorView =
  | { kind: "invalid_isbn" }
  | { kind: "rate_limited"; seconds: number | null }
  | { kind: "not_found" }
  | { kind: "borrow_limit" }
  | { kind: "forbidden" }
  | { kind: "conflict"; currentStatus: string | null }
  | { kind: "generic" };

/** Maps a failed scan/return call onto the docs/11 §8 error table. */
export function scanErrorView(err: unknown, nowMs = Date.now()): ScanErrorView {
  if (!(err instanceof ApiError)) return { kind: "generic" };
  if (err.status === 429)
    return { kind: "rate_limited", seconds: rateLimitSeconds(err.headers, nowMs) };
  if (err.status === 400 && err.code === "INVALID_ISBN") return { kind: "invalid_isbn" };
  if (err.status === 404) return { kind: "not_found" };
  if (err.status === 403) return { kind: "forbidden" };
  if (err.status === 409 && err.code === "BORROW_LIMIT") return { kind: "borrow_limit" };
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
    borrowable: ret.copy.status === "available",
    loan: null,
  };
}

/** Result after a 409 CONFLICT: reflect the copy's real status (docs/11 §8). */
export function applyConflict(result: ScanResponse, currentStatus: string | null): ScanResponse {
  if (!result.copy || !currentStatus) return { ...result, returnable: false };
  return {
    ...result,
    copy: { ...result.copy, status: currentStatus },
    returnable: currentStatus === "checked_out",
    borrowable: currentStatus === "available",
  };
}

/** True when the waitlist CTA replaces "copies left" (docs/07 §Scanner dialog). */
export function needsWaitlist(result: ScanResponse): boolean {
  return result.match === "book" && (result.availability?.available_copies ?? 0) === 0;
}

/** True when an unknown, checksum-valid ISBN-13 should be identified via Open Library (docs/11 §2a). */
export function shouldIdentify(result: ScanResponse): boolean {
  return result.code_type === "isbn13" && result.match === "none";
}

/**
 * Result after a successful book-level borrow (POST /api/books/{id}/borrow).
 * The server picked the copy; we update borrow_copy, availability and my_loans.
 */
export function applyBookBorrow(result: ScanResponse, res: BorrowBookResponse): ScanResponse {
  return {
    ...result,
    borrowable: false,
    borrow_copy: null,
    returnable: true,
    my_loans: [
      {
        id: res.loan.id,
        copy_id: res.loan.copy_id,
        barcode: res.copy.barcode,
        due_at: res.loan.due_at,
      },
      ...(result.my_loans ?? []),
    ],
    availability: res.availability,
  };
}

/**
 * Result after a successful book-level return (POST /api/books/{id}/return).
 * Removes the oldest loan and refreshes availability.
 */
export function applyBookReturn(result: ScanResponse, res: ReturnBookResponse): ScanResponse {
  const [, ...rest] = result.my_loans ?? [];
  return {
    ...result,
    returnable: rest.length > 0,
    borrowable: res.availability.available_copies > 0,
    my_loans: rest,
    availability: res.availability,
    borrow_copy: null,
  };
}

export type IdentifyView =
  | { kind: "identified"; book: NonNullable<IdentifyResponse["identified"]> }
  | { kind: "in_catalog"; bookId: string }
  | { kind: "unidentified" };

/** Maps an identify response onto the card state; anything else stays "unidentified". */
export function identifyView(res: IdentifyResponse | null): IdentifyView {
  if (!res) return { kind: "unidentified" };
  if (res.in_catalog && res.book_id) return { kind: "in_catalog", bookId: res.book_id };
  if (res.identified && res.identified.title) return { kind: "identified", book: res.identified };
  return { kind: "unidentified" };
}
