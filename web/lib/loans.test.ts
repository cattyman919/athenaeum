import { describe, expect, it } from "vitest";
import { daysUntil, isDueSoon, sortLoans } from "./loans";
import type { Loan } from "./types";

const now = Date.parse("2026-10-16T00:00:00Z");
const mk = (p: Partial<Loan>): Loan => ({
  kind: "active",
  id: "1",
  copy_id: "c",
  barcode: "ATH-1",
  book: { id: "b", title: "T", cover_url: null, authors: [] },
  borrowed_at: "2026-10-01T00:00:00Z",
  due_at: "2026-10-20T00:00:00Z",
  returned_at: null,
  renewals: 0,
  overdue: false,
  renewable: true,
  ...p,
});

describe("loans helpers", () => {
  it("computes days until due", () => {
    expect(daysUntil("2026-10-19T00:00:00Z", now)).toBe(3);
    expect(daysUntil("2026-10-15T00:00:00Z", now)).toBe(-1);
  });
  it("flags due soon only for active, non-overdue loans", () => {
    expect(isDueSoon(mk({ due_at: "2026-10-18T00:00:00Z" }), now)).toBe(true);
    expect(isDueSoon(mk({ due_at: "2026-10-30T00:00:00Z" }), now)).toBe(false);
    expect(isDueSoon(mk({ overdue: true, due_at: "2026-10-10T00:00:00Z" }), now)).toBe(false);
    expect(isDueSoon(mk({ kind: "past" }), now)).toBe(false);
  });
  it("sorts active by due then past by return", () => {
    const out = sortLoans([
      mk({ id: "p", kind: "past", returned_at: "2026-10-02T00:00:00Z" }),
      mk({ id: "b", due_at: "2026-10-25T00:00:00Z" }),
      mk({ id: "a", due_at: "2026-10-18T00:00:00Z" }),
    ]);
    expect(out.map((l) => l.id)).toEqual(["a", "b", "p"]);
  });
});
