import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ancestorIds,
  canChangeRole,
  createPoller,
  locateShelf,
  mergeEvents,
  minimapLayout,
  nextSince,
  normalizeBarcode,
  occupancyLevel,
  pct,
  rollup,
  statusLabel,
  statusTone,
} from "./admin";
import type { AdminUser, CopyEvent } from "./admin-types";
import type { AdminTree } from "./intake-types";

const ev = (id: number, created_at: string, to = "available"): CopyEvent => ({
  id,
  barcode: `ATH-0029${id}-C01`,
  book_title: "Dune",
  from_status: "checked_out",
  to_status: to,
  source: "admin_patch",
  actor: null,
  created_at,
});

describe("mergeEvents", () => {
  it("dedupes by id and orders newest first", () => {
    const cur = [ev(2, "2026-10-06T10:00:02Z"), ev(1, "2026-10-06T10:00:01Z")];
    const merged = mergeEvents(cur, [ev(3, "2026-10-06T10:00:03Z"), ev(2, "2026-10-06T10:00:02Z")]);
    expect(merged.map((e) => e.id)).toEqual([3, 2, 1]);
  });

  it("breaks timestamp ties by id and caps the list", () => {
    const t = "2026-10-06T10:00:00Z";
    const merged = mergeEvents([], [ev(1, t), ev(5, t), ev(3, t)], 2);
    expect(merged.map((e) => e.id)).toEqual([5, 3]);
  });

  it("returns the same array for an empty batch", () => {
    const cur = [ev(1, "2026-10-06T10:00:00Z")];
    expect(mergeEvents(cur, [])).toBe(cur);
  });
});

describe("nextSince", () => {
  it("keeps the previous cursor when nothing is newer", () => {
    expect(nextSince([], null)).toBeNull();
    expect(nextSince([], "2026-10-06T10:00:00Z")).toBe("2026-10-06T10:00:00Z");
    expect(nextSince([ev(1, "2026-10-06T09:00:00Z")], "2026-10-06T10:00:00Z")).toBe(
      "2026-10-06T10:00:00Z",
    );
  });

  it("advances to the newest created_at, preserving sub-second precision", () => {
    const events = [ev(1, "2026-10-06T10:00:00.5Z"), ev(2, "2026-10-06T10:00:01.123456Z")];
    expect(nextSince(events, null)).toBe("2026-10-06T10:00:01.123456Z");
  });
});

describe("createPoller", () => {
  afterEach(() => vi.useRealTimers());

  function fakeDoc() {
    let fn: (() => void) | null = null;
    return {
      hidden: false,
      addEventListener: (_: "visibilitychange", f: () => void) => (fn = f),
      removeEventListener: () => (fn = null),
      fire() {
        fn?.();
      },
    };
  }

  it("ticks every interval and pauses while hidden", async () => {
    vi.useFakeTimers();
    const doc = fakeDoc();
    const tick = vi.fn(async () => {});
    const p = createPoller(tick, 3000, {
      setTimeout: (f, ms) => setTimeout(f, ms),
      clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
      doc,
    });
    p.start();
    await vi.advanceTimersByTimeAsync(3000);
    expect(tick).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(3000);
    expect(tick).toHaveBeenCalledTimes(2);

    doc.hidden = true;
    doc.fire();
    expect(p.active).toBe(false);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(tick).toHaveBeenCalledTimes(2);

    doc.hidden = false;
    doc.fire(); // immediate catch-up tick on return
    await vi.advanceTimersByTimeAsync(0);
    expect(tick).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(3000);
    expect(tick).toHaveBeenCalledTimes(4);

    p.stop();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(tick).toHaveBeenCalledTimes(4);
  });

  it("survives a failing tick", async () => {
    vi.useFakeTimers();
    const tick = vi.fn().mockRejectedValueOnce(new Error("down")).mockResolvedValue(undefined);
    const p = createPoller(tick, 1000, {
      setTimeout: (f, ms) => setTimeout(f, ms),
      clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
    });
    p.start();
    await vi.advanceTimersByTimeAsync(2000);
    expect(tick).toHaveBeenCalledTimes(2);
    p.stop();
  });
});

describe("labels and ratios", () => {
  it("labels statuses in English", () => {
    expect(statusLabel("missing_lost")).toBe("Missing / lost");
    expect(statusLabel(null)).toBe("Registered");
    expect(statusLabel("weird")).toBe("weird");
    expect(statusTone("checked_out")).toBe("out");
    expect(statusTone("missing_lost")).toBe("unavailable");
  });

  it("computes percentages and occupancy levels", () => {
    expect(pct(48, 60)).toBe(80);
    expect(pct(1, 0)).toBe(0);
    expect(occupancyLevel({ occupied: 0, total: 15 })).toBe("empty");
    expect(occupancyLevel({ occupied: 2, total: 15 })).toBe("low");
    expect(occupancyLevel({ occupied: 8, total: 15 })).toBe("mid");
    expect(occupancyLevel({ occupied: 12, total: 15 })).toBe("high");
    expect(occupancyLevel({ occupied: 15, total: 15 })).toBe("full");
  });

  it("normalizes barcodes", () => {
    expect(normalizeBarcode("  ath-002941-c02 ")).toBe("ATH-002941-C02");
    expect(normalizeBarcode("   ")).toBeNull();
  });
});

const shelf = (id: string, code: string, occupied: number) => ({
  id,
  code,
  path: `MAIN/3F/SF/A-1/${code}`,
  row_level: 4,
  height_tier: "Eye Level",
  occupied,
  total: 15,
});

const tree: AdminTree = {
  id: "b",
  code: "MAIN",
  name: "Main Library",
  floors: [
    {
      id: "f3",
      floor_number: 3,
      name: "North Stacks",
      sections: [
        {
          id: "sf",
          code: "SF",
          name: "Science Fiction",
          color: "#3B82F6",
          aisles: [
            { id: "a1", code: "A-1", shelves: [shelf("s1", "S-1", 2), shelf("s2", "S-2", 3)] },
            { id: "a2", code: "A-2", shelves: [shelf("s3", "S-1", 1)] },
          ],
        },
        {
          id: "per",
          code: "PER",
          name: "Periodicals",
          color: "#F59E0B",
          aisles: [{ id: "a3", code: "A-1", shelves: [shelf("s4", "S-1", 0)] }],
        },
      ],
    },
  ],
};

describe("building tree", () => {
  it("locates a shelf with its ancestors", () => {
    const loc = locateShelf(tree, "s3");
    expect(loc?.aisle.code).toBe("A-2");
    expect(loc?.section.code).toBe("SF");
    expect(ancestorIds(tree, "s3")).toEqual(["f3", "sf", "a2"]);
    expect(locateShelf(tree, "nope")).toBeNull();
    expect(ancestorIds(tree, "nope")).toEqual([]);
  });

  it("rolls up occupancy", () => {
    expect(rollup(tree.floors[0].sections[0].aisles.flatMap((a) => a.shelves))).toEqual({
      occupied: 6,
      total: 45,
    });
  });

  it("lays out the mini-map with one column per aisle and one cell per shelf", () => {
    const m = minimapLayout(tree.floors[0]);
    expect(m.sections.map((s) => s.code)).toEqual(["SF", "PER"]);
    expect(m.aisles).toHaveLength(3);
    expect(m.shelves).toHaveLength(4);
    // Sections do not overlap and fit in the canvas.
    const [sf, per] = m.sections;
    expect(per.x).toBeGreaterThan(sf.x + sf.w);
    expect(per.x + per.w).toBeLessThanOrEqual(m.width);
    // Shelves stack downwards inside their aisle column.
    const [c1, c2] = m.shelves.filter((s) => s.aisleId === "a1");
    expect(c2.y).toBeGreaterThan(c1.y + c1.h);
    const a1 = m.aisles.find((a) => a.id === "a1")!;
    expect(c2.y + c2.h).toBeLessThanOrEqual(a1.y + a1.h);
  });
});

describe("canChangeRole", () => {
  const u = (id: string, role: AdminUser["role"]): AdminUser => ({
    id,
    email: `${id}@x`,
    name: id,
    role,
    created_at: "",
    library_count: 0,
    waitlist_count: 0,
    last_login_at: null,
  });

  it("blocks self-demotion and no-op changes", () => {
    expect(canChangeRole("me", u("me", "admin"), "user")).toBe(false);
    expect(canChangeRole("me", u("me", "admin"), "admin")).toBe(false);
    expect(canChangeRole("me", u("you", "user"), "admin")).toBe(true);
    expect(canChangeRole("me", u("you", "admin"), "user")).toBe(true);
  });
});
