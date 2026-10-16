import { afterEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "./api-client";
import { createPendingWatcher, indexingLabel, pendingSet, INDEX_POLL_MS } from "./index-status";
import type { IndexStatus } from "./admin-types";

const ok = (body: unknown) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });

const status = (...ids: string[]): IndexStatus => ({
  pending_count: ids.length,
  pending_book_ids: ids,
});

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

const timers = {
  setTimeout: (f: () => void, ms: number) => setTimeout(f, ms),
  clearTimeout: (h: unknown) => clearTimeout(h as ReturnType<typeof setTimeout>),
};

describe("createPendingWatcher", () => {
  afterEach(() => vi.useRealTimers());

  it("checks immediately, polls every 4 s while pending and stops at zero", async () => {
    vi.useFakeTimers();
    const queue = [status("a", "b"), status("b"), status()];
    const check = vi.fn(async () => queue.shift() ?? status());
    const seen: number[] = [];
    const w = createPendingWatcher({
      check,
      count: (s) => s.pending_count,
      onResult: (s) => seen.push(s.pending_count),
      deps: { ...timers, doc: fakeDoc() },
    });
    w.refresh();
    await vi.advanceTimersByTimeAsync(0);
    expect(seen).toEqual([2]);
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS);
    expect(seen).toEqual([2, 1]);
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS);
    expect(seen).toEqual([2, 1, 0]);
    expect(w.active).toBe(false);
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS * 5);
    expect(check).toHaveBeenCalledTimes(3);
  });

  it("does not poll when the first check finds nothing pending; refresh restarts it", async () => {
    vi.useFakeTimers();
    const queue = [status(), status("new"), status()];
    const check = vi.fn(async () => queue.shift() ?? status());
    const w = createPendingWatcher({
      check,
      count: (s) => s.pending_count,
      onResult: () => {},
      deps: timers,
    });
    w.refresh();
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS * 3);
    expect(check).toHaveBeenCalledTimes(1);

    w.refresh(); // e.g. after the Add Book flow created a book
    await vi.advanceTimersByTimeAsync(0);
    expect(w.active).toBe(true);
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS);
    expect(check).toHaveBeenCalledTimes(3);
    expect(w.active).toBe(false);
  });

  it("pauses while hidden and stops for good on stop()", async () => {
    vi.useFakeTimers();
    const doc = fakeDoc();
    const check = vi.fn(async () => status("a"));
    const w = createPendingWatcher({
      check,
      count: (s) => s.pending_count,
      onResult: () => {},
      deps: { ...timers, doc },
    });
    w.refresh();
    await vi.advanceTimersByTimeAsync(0);
    doc.hidden = true;
    doc.fire();
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS * 5);
    expect(check).toHaveBeenCalledTimes(1);
    doc.hidden = false;
    doc.fire();
    await vi.advanceTimersByTimeAsync(0);
    expect(check).toHaveBeenCalledTimes(2);

    w.stop();
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS * 5);
    expect(check).toHaveBeenCalledTimes(2);
  });

  it("ignores a superseded result and keeps polling after a failed check", async () => {
    vi.useFakeTimers();
    let resolveSlow: (s: IndexStatus) => void = () => {};
    const check = vi
      .fn<() => Promise<IndexStatus>>()
      .mockImplementationOnce(() => new Promise((r) => (resolveSlow = r)))
      .mockImplementationOnce(async () => status("x"))
      .mockImplementationOnce(async () => {
        throw new Error("down");
      })
      .mockImplementation(async () => status());
    const seen: number[] = [];
    const w = createPendingWatcher({
      check,
      count: (s) => s.pending_count,
      onResult: (s) => seen.push(s.pending_count),
      deps: timers,
    });
    w.refresh();
    w.refresh();
    await vi.advanceTimersByTimeAsync(0);
    resolveSlow(status()); // stale "0" must not stop the newer poll
    await vi.advanceTimersByTimeAsync(0);
    expect(seen).toEqual([1]);
    expect(w.active).toBe(true);
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS); // throws → retried next interval
    expect(w.active).toBe(true);
    await vi.advanceTimersByTimeAsync(INDEX_POLL_MS);
    expect(seen).toEqual([1, 0]);
    expect(w.active).toBe(false);
  });
});

describe("index-status helpers", () => {
  it("builds the pending id set", () => {
    expect([...pendingSet(status("a", "b"))]).toEqual(["a", "b"]);
  });

  it("pluralizes the English chip label", () => {
    expect(indexingLabel(1)).toBe("1 book indexing");
    expect(indexingLabel(3)).toBe("3 books indexing");
  });
});

describe("api client index-status methods", () => {
  it("GETs /api/admin/index-status and search with index_pending", async () => {
    const urls: string[] = [];
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      urls.push(url);
      return url.startsWith("/api/admin/index-status")
        ? ok(status("b1"))
        : ok({
            items: [],
            meta: { page: 1, per_page: 1, total: 0, total_pages: 0, index_pending: 2 },
          });
    });
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    expect(await c.adminIndexStatus()).toEqual(status("b1"));
    const res = await c.searchBooks(new URLSearchParams({ mode: "keyword", per_page: "1" }));
    expect(res.meta.index_pending).toBe(2);
    expect(urls).toEqual(["/api/admin/index-status", "/api/books/search?mode=keyword&per_page=1"]);
  });
});
