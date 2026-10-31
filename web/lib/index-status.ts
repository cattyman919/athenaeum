// Auto-index backlog watching (docs/07 §7 screen 6a, docs/06 §2): the admin polls
// GET /api/admin/index-status and patrons re-check `meta.index_pending` while books are
// still being embedded. Side-effect free (timers/document injected) for vitest.

import { createPoller, type PollerDeps } from "./admin";
import type { IndexStatus } from "./admin-types";

export const INDEX_POLL_MS = 4000;

/** Window event the Add Book flow fires after POST /api/admin/books succeeds. */
export const BOOK_CREATED_EVENT = "athenaeum:book-created";
export const BOOKS_REMOVED_EVENT = "athenaeum:books-removed";

export interface BookCreatedDetail {
  id: string;
  title: string;
}

export interface PendingWatcherOptions<T> {
  check: () => Promise<T>;
  count: (result: T) => number;
  onResult: (result: T) => void;
  deps: PollerDeps;
  intervalMs?: number;
}

/**
 * Checks now, then keeps polling every interval while `count(result) > 0`; stops itself
 * once the backlog is empty (until the next `refresh`). Pauses while the tab is hidden.
 * Results of superseded checks are dropped so a late "0" cannot stop a newer poll.
 */
export function createPendingWatcher<T>(opts: PendingWatcherOptions<T>) {
  let seq = 0;
  const tick = async () => {
    const mine = ++seq;
    const result = await opts.check();
    if (mine !== seq) return;
    opts.onResult(result);
    if (opts.count(result) <= 0) poller.stop();
  };
  const poller = createPoller(tick, opts.intervalMs ?? INDEX_POLL_MS, opts.deps);

  return {
    refresh() {
      poller.start();
      tick().catch(() => {
        // The poller retries on the next interval.
      });
    },
    stop() {
      seq++;
      poller.stop();
    },
    get active() {
      return poller.active;
    },
  };
}

/** Normalizes an index-status payload into the pending-id set the badges read. */
export function pendingSet(status: IndexStatus): Set<string> {
  return new Set(status.pending_book_ids ?? []);
}

/** English chip label (admin stays English — docs/14 §1). */
export function indexingLabel(count: number): string {
  return `${count} ${count === 1 ? "book" : "books"} indexing`;
}
