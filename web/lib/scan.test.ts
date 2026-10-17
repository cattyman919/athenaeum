import { describe, expect, it, vi } from "vitest";
import { ApiError, createApiClient } from "./api-client";
import {
  applyBookBorrow,
  applyBookReturn,
  applyConflict,
  applyReturn,
  identifyView,
  needsWaitlist,
  scanErrorView,
  shouldIdentify,
} from "./scan";
import type { BorrowBookResponse, ReturnBookResponse, ReturnResponse, ScanResponse } from "./types";

const parts = {
  building_code: "MAIN",
  building_name: "Athenaeum Main Building",
  floor_number: 1,
  floor_name: "Reading Hall",
  section_code: "SCI",
  section_name: "Science, Mathematics & Tech",
  aisle_code: "A-3",
  shelf_code: "S-2",
  row_level: 2,
  height_tier: "Eye Level",
};
const location = { path: "MAIN/1F/SCI/A-3/S-2", parts, ladder_steps: [] };

const copyScan: ScanResponse = {
  code: "ATH-002960-C01",
  code_type: "copy_barcode",
  in_catalog: true,
  match: "copy",
  book: {
    id: "b20",
    title: "The C Programming Language",
    authors: ["Brian W. Kernighan", "Dennis M. Ritchie"],
    isbn13: "9780131103627",
    cover_url: null,
    section: null,
  },
  copy: {
    id: "c1",
    barcode: "ATH-002960-C01",
    status: "checked_out",
    condition: "good",
    location,
  },
  availability: { total_copies: 2, available_copies: 1, state: "available" },
  wayfinding: location,
  returnable: true,
};

const headers = (h: Record<string, string>) => new Headers(h);

describe("scanErrorView (docs/11 §8)", () => {
  it("maps the documented API errors", () => {
    expect(scanErrorView(new ApiError(400, "INVALID_ISBN", "bad"))).toEqual({
      kind: "invalid_isbn",
    });
    expect(scanErrorView(new ApiError(404, "NOT_FOUND", "gone"))).toEqual({ kind: "not_found" });
    expect(
      scanErrorView(
        new ApiError(409, "CONFLICT", "Copy is not checked out", {
          error: "CONFLICT",
          current_status: "available",
        }),
      ),
    ).toEqual({ kind: "conflict", currentStatus: "available" });
    expect(scanErrorView(new ApiError(409, "CONFLICT", "x"))).toEqual({
      kind: "conflict",
      currentStatus: null,
    });
    expect(scanErrorView(new Error("network"))).toEqual({ kind: "generic" });
    expect(scanErrorView(new ApiError(500, "INTERNAL", "x"))).toEqual({ kind: "generic" });
  });

  it("reports rate-limit reset seconds", () => {
    const err = new ApiError(
      429,
      "RATE_LIMITED",
      "slow",
      undefined,
      headers({
        "X-RateLimit-Reset": "1012",
      }),
    );
    expect(scanErrorView(err, 1_000_000)).toEqual({ kind: "rate_limited", seconds: 12 });
    expect(scanErrorView(new ApiError(429, "RATE_LIMITED", "slow"))).toEqual({
      kind: "rate_limited",
      seconds: null,
    });
  });
});

describe("result updates", () => {
  it("applyReturn reflects the returned copy and fresh availability", () => {
    const ret: ReturnResponse = {
      copy: { ...copyScan.copy!, status: "available" },
      availability: { total_copies: 2, available_copies: 2, state: "available" },
      hold: null,
    };
    const next = applyReturn(copyScan, ret);
    expect(next.copy?.status).toBe("available");
    expect(next.availability?.available_copies).toBe(2);
    expect(next.returnable).toBe(false);
  });

  it("applyConflict refreshes to the copy's real status", () => {
    const next = applyConflict(copyScan, "reserved_waitlist");
    expect(next.copy?.status).toBe("reserved_waitlist");
    expect(next.returnable).toBe(false);
    expect(applyConflict(copyScan, null).returnable).toBe(false);
  });

  it("needsWaitlist only for a book match with zero available copies", () => {
    expect(needsWaitlist(copyScan)).toBe(false);
    const book: ScanResponse = {
      ...copyScan,
      match: "book",
      copy: null,
      returnable: false,
      availability: { total_copies: 1, available_copies: 0, state: "checked_out" },
    };
    expect(needsWaitlist(book)).toBe(true);
    expect(
      needsWaitlist({ ...book, availability: { ...book.availability!, available_copies: 1 } }),
    ).toBe(false);
  });
});

describe("api client scanner calls (docs/03 §5.4, §6.4)", () => {
  it("posts the code to /api/books/scan and the copy id to /return", async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push([String(input), init]);
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    await c.scanCode("9780441013593");
    await c.returnCopy("c 1");
    expect(calls[0][0]).toBe("/api/books/scan");
    expect(calls[0][1]?.method).toBe("POST");
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ code: "9780441013593" });
    expect(calls[1][0]).toBe("/api/copies/c%201/return");
    expect(calls[1][1]?.method).toBe("POST");
    expect(calls[1][1]?.body).toBeUndefined();
  });
});

const bookScan: ScanResponse = {
  code: "9780553380958",
  code_type: "isbn13",
  in_catalog: true,
  match: "book",
  book: {
    id: "b6",
    title: "Snow Crash",
    authors: ["Neal Stephenson"],
    isbn13: "9780553380958",
    cover_url: null,
    section: null,
  },
  copy: null,
  availability: { total_copies: 3, available_copies: 3, state: "available" },
  wayfinding: location,
  returnable: false,
  borrowable: true,
  borrow_copy: { id: "c-sc1", barcode: "ATH-002946-C01", location },
  my_loans: [],
};

describe("book-level borrow/return helpers (docs/03 §6.5)", () => {
  it("applyBookBorrow clears borrowable, adds the loan to my_loans", () => {
    const res: BorrowBookResponse = {
      loan: {
        id: "loan-1",
        copy_id: "c-sc1",
        user_id: "u1",
        borrowed_at: "2026-10-17T06:00:00Z",
        due_at: "2026-10-31T06:00:00Z",
        returned_at: null,
        renewals: 0,
        renewable: true,
      },
      copy: {
        id: "c-sc1",
        barcode: "ATH-002946-C01",
        status: "checked_out",
        condition: "good",
        location,
      },
      availability: { total_copies: 3, available_copies: 2, state: "available" },
    };
    const next = applyBookBorrow(bookScan, res);
    expect(next.borrowable).toBe(false);
    expect(next.returnable).toBe(true);
    expect(next.my_loans).toHaveLength(1);
    expect(next.my_loans![0].barcode).toBe("ATH-002946-C01");
    expect(next.availability?.available_copies).toBe(2);
  });

  it("applyBookReturn removes the oldest loan and refreshes availability", () => {
    const withLoan: ScanResponse = {
      ...bookScan,
      returnable: true,
      borrowable: false,
      my_loans: [
        {
          id: "loan-1",
          copy_id: "c-sc1",
          barcode: "ATH-002946-C01",
          due_at: "2026-10-31T06:00:00Z",
        },
      ],
    };
    const res: ReturnBookResponse = {
      copy: {
        id: "c-sc1",
        barcode: "ATH-002946-C01",
        status: "available",
        condition: "good",
        location,
      },
      availability: { total_copies: 3, available_copies: 3, state: "available" },
      hold: null,
      closed_loan: { id: "loan-1" },
    };
    const next = applyBookReturn(withLoan, res);
    expect(next.returnable).toBe(false);
    expect(next.my_loans).toHaveLength(0);
    expect(next.availability?.available_copies).toBe(3);
  });
});

describe("identify helpers", () => {
  it("shouldIdentify only for unknown ISBN-13s", () => {
    expect(shouldIdentify({ ...copyScan, code_type: "isbn13", match: "none" })).toBe(true);
    expect(shouldIdentify({ ...copyScan, code_type: "isbn13", match: "book" })).toBe(false);
    expect(shouldIdentify({ ...copyScan, code_type: "copy_barcode", match: "none" })).toBe(false);
  });

  it("identifyView maps responses", () => {
    const base = { isbn13: "9780553293357", source: "openlibrary" as const, warnings: [] };
    expect(identifyView(null).kind).toBe("unidentified");
    expect(
      identifyView({
        ...base,
        in_catalog: false,
        book_id: null,
        identified: {
          title: "Foundation",
          authors: [],
          publisher: null,
          published_year: null,
          cover_url: null,
        },
      }).kind,
    ).toBe("identified");
    expect(identifyView({ ...base, in_catalog: true, book_id: "b1", identified: null }).kind).toBe(
      "in_catalog",
    );
    expect(identifyView({ ...base, in_catalog: false, book_id: null, identified: null }).kind).toBe(
      "unidentified",
    );
  });
});
