// Pure admin-portal logic (docs/07 §7 screens 6a/6b): audit-stream merging + polling,
// KPI ratios, building-tree lookups, the floor mini-map layout and role guards.
// Side-effect free (the poller takes its timers/document as arguments) for vitest.

import type { AdminTree, AdminTreeShelf } from "./intake-types";
import type { AdminUser, CopyEvent, Occupancy, UserRole } from "./admin-types";

// ---- audit stream ------------------------------------------------------------------

export const AUDIT_POLL_MS = 3000;
export const AUDIT_MAX_ROWS = 50;

function newer(a: CopyEvent, b: CopyEvent): number {
  const ta = Date.parse(a.created_at);
  const tb = Date.parse(b.created_at);
  if (ta !== tb) return tb - ta;
  return b.id - a.id;
}

/** Merges a poll batch into the stream: de-duplicated by id, newest first, capped. */
export function mergeEvents(
  current: CopyEvent[],
  incoming: CopyEvent[],
  max = AUDIT_MAX_ROWS,
): CopyEvent[] {
  if (incoming.length === 0) return current;
  const byId = new Map<number, CopyEvent>();
  for (const e of current) byId.set(e.id, e);
  for (const e of incoming) byId.set(e.id, e);
  return [...byId.values()].sort(newer).slice(0, max);
}

/**
 * The `since` cursor for the next poll: the newest `created_at` seen (the API filters
 * `created_at > since`), or the previous cursor when nothing is loaded yet.
 */
export function nextSince(events: CopyEvent[], prev: string | null): string | null {
  let best = prev;
  let bestMs = prev ? Date.parse(prev) : -Infinity;
  for (const e of events) {
    const ms = Date.parse(e.created_at);
    if (ms > bestMs) {
      best = e.created_at;
      bestMs = ms;
    }
  }
  return best;
}

export interface PollerDeps {
  setTimeout: (fn: () => void, ms: number) => unknown;
  clearTimeout: (handle: unknown) => void;
  /** `document`-like visibility source; omitted → always visible. */
  doc?: {
    hidden: boolean;
    addEventListener: (type: "visibilitychange", fn: () => void) => void;
    removeEventListener: (type: "visibilitychange", fn: () => void) => void;
  };
}

/**
 * Sequential poller: runs `tick`, waits `intervalMs` after it settles, repeats. Pauses
 * while the tab is hidden and ticks immediately when it becomes visible again.
 */
export function createPoller(tick: () => Promise<void>, intervalMs: number, deps: PollerDeps) {
  let handle: unknown = null;
  let running = false;
  let inFlight = false;

  const schedule = () => {
    if (!running || deps.doc?.hidden) return;
    handle = deps.setTimeout(run, intervalMs);
  };

  async function run() {
    handle = null;
    if (!running || inFlight || deps.doc?.hidden) return;
    inFlight = true;
    try {
      await tick();
    } catch {
      // A failed poll keeps the stream alive; the next tick retries.
    } finally {
      inFlight = false;
    }
    schedule();
  }

  const onVisibility = () => {
    if (!running) return;
    if (deps.doc?.hidden) {
      if (handle !== null) deps.clearTimeout(handle);
      handle = null;
    } else if (handle === null && !inFlight) {
      void run();
    }
  };

  return {
    start() {
      if (running) return;
      running = true;
      deps.doc?.addEventListener("visibilitychange", onVisibility);
      schedule();
    },
    stop() {
      running = false;
      if (handle !== null) deps.clearTimeout(handle);
      handle = null;
      deps.doc?.removeEventListener("visibilitychange", onVisibility);
    },
    get active() {
      return running && !deps.doc?.hidden;
    },
  };
}

// ---- labels & ratios ---------------------------------------------------------------

const STATUS_LABELS: Record<string, string> = {
  available: "Available",
  checked_out: "Checked out",
  reserved_waitlist: "Reserved (waitlist)",
  missing_lost: "Missing / lost",
};

/** English copy-status label (admin stays English — docs/14 §1). */
export function statusLabel(status: string | null): string {
  if (status === null) return "Registered";
  return STATUS_LABELS[status] ?? status;
}

export type StatusTone = "available" | "out" | "unavailable" | "default";

export function statusTone(status: string | null): StatusTone {
  switch (status) {
    case "available":
      return "available";
    case "checked_out":
    case "reserved_waitlist":
      return "out";
    case "missing_lost":
      return "unavailable";
    default:
      return "default";
  }
}

/** Integer percentage, 0 for an empty denominator. */
export function pct(part: number, whole: number): number {
  if (whole <= 0) return 0;
  return Math.round((part / whole) * 100);
}

export type OccupancyLevel = "empty" | "low" | "mid" | "high" | "full";

export function occupancyLevel(o: Occupancy): OccupancyLevel {
  if (o.occupied <= 0) return "empty";
  if (o.total > 0 && o.occupied >= o.total) return "full";
  const p = pct(o.occupied, o.total);
  if (p < 34) return "low";
  if (p < 67) return "mid";
  return "high";
}

/** Normalizes a typed/scanned barcode for quick-assign; null when blank. */
export function normalizeBarcode(input: string): string | null {
  const v = input.trim().toUpperCase();
  return v === "" ? null : v;
}

// ---- building tree -----------------------------------------------------------------

type Floor = AdminTree["floors"][number];
type Section = Floor["sections"][number];
type Aisle = Section["aisles"][number];

export interface ShelfLocation {
  floor: Floor;
  section: Section;
  aisle: Aisle;
  shelf: AdminTreeShelf;
}

/** Finds a shelf by id, with its ancestors. */
export function locateShelf(tree: AdminTree, shelfId: string): ShelfLocation | null {
  for (const floor of tree.floors)
    for (const section of floor.sections)
      for (const aisle of section.aisles)
        for (const shelf of aisle.shelves)
          if (shelf.id === shelfId) return { floor, section, aisle, shelf };
  return null;
}

/** Tree-node ids to expand so `shelfId` is visible (floor, section, aisle). */
export function ancestorIds(tree: AdminTree, shelfId: string): string[] {
  const loc = locateShelf(tree, shelfId);
  return loc ? [loc.floor.id, loc.section.id, loc.aisle.id] : [];
}

/** Occupancy rolled up over every shelf below a node. */
export function rollup(shelves: AdminTreeShelf[]): Occupancy {
  return shelves.reduce(
    (acc, s) => ({ occupied: acc.occupied + s.occupied, total: acc.total + s.total }),
    { occupied: 0, total: 0 },
  );
}

export function floorShelves(floor: Floor): AdminTreeShelf[] {
  return floor.sections.flatMap((s) => s.aisles.flatMap((a) => a.shelves));
}

export function sectionShelves(section: Section): AdminTreeShelf[] {
  return section.aisles.flatMap((a) => a.shelves);
}

// ---- floor mini-map ----------------------------------------------------------------

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface MiniMapLayout {
  width: number;
  height: number;
  sections: Array<Rect & { id: string; code: string; color: string }>;
  aisles: Array<Rect & { id: string; code: string; sectionId: string }>;
  shelves: Array<Rect & { id: string; code: string; aisleId: string; occupancy: Occupancy }>;
}

export const MINIMAP = { pad: 8, label: 14, gap: 6, aisleW: 22, cellH: 12, cellGap: 3 } as const;

/**
 * Simple SVG grid for one floor (docs/07 screen 6b): sections side by side, each aisle a
 * column, each shelf a cell stacked in the column.
 */
export function minimapLayout(floor: Floor): MiniMapLayout {
  const { pad, label, gap, aisleW, cellH, cellGap } = MINIMAP;
  const maxShelves = Math.max(
    1,
    ...floor.sections.flatMap((s) => s.aisles.map((a) => a.shelves.length)),
  );
  const columnH = maxShelves * cellH + (maxShelves - 1) * cellGap;
  const sectionH = label + columnH + pad * 2;
  const out: MiniMapLayout = {
    width: 0,
    height: sectionH + pad * 2,
    sections: [],
    aisles: [],
    shelves: [],
  };

  let x = pad;
  for (const section of floor.sections) {
    const n = Math.max(1, section.aisles.length);
    const w = pad * 2 + n * aisleW + (n - 1) * gap;
    out.sections.push({
      id: section.id,
      code: section.code,
      color: section.color,
      x,
      y: pad,
      w,
      h: sectionH,
    });
    section.aisles.forEach((aisle, i) => {
      const ax = x + pad + i * (aisleW + gap);
      const ay = pad + pad + label;
      out.aisles.push({
        id: aisle.id,
        code: aisle.code,
        sectionId: section.id,
        x: ax,
        y: ay,
        w: aisleW,
        h: columnH,
      });
      aisle.shelves.forEach((shelf, j) => {
        out.shelves.push({
          id: shelf.id,
          code: shelf.code,
          aisleId: aisle.id,
          occupancy: { occupied: shelf.occupied, total: shelf.total },
          x: ax + 2,
          y: ay + j * (cellH + cellGap),
          w: aisleW - 4,
          h: cellH,
        });
      });
    });
    x += w + gap;
  }
  out.width = x - gap + pad;
  return out;
}

// ---- users -------------------------------------------------------------------------

/** Client guard mirroring the server's 400 on self-demotion (docs/03 §8.5). */
export function canChangeRole(actorId: string, target: AdminUser, next: UserRole): boolean {
  if (target.role === next) return false;
  return !(target.id === actorId && next !== "admin");
}
