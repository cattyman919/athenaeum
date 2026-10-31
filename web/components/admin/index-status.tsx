"use client";

// Auto-index backlog UI for /admin and /admin/layout (docs/07 §7 screens 6a/6b, docs/06 §2).
// The provider polls GET /api/admin/index-status every 4 s while mounted and visible, stops
// once nothing is pending, and restarts when the Add Book flow creates a book
// (BOOK_CREATED_EVENT). Admin copy stays English (docs/14 §1).

import * as React from "react";
import Link from "next/link";
import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api-client";
import {
  BOOK_CREATED_EVENT,
  BOOKS_REMOVED_EVENT,
  createPendingWatcher,
  indexingLabel,
  pendingSet,
  type BookCreatedDetail,
} from "@/lib/index-status";
import { cn } from "@/lib/utils";

interface IndexStatusValue {
  pendingCount: number;
  isPending: (bookId: string) => boolean;
  /** Books created through the Add Book flow while this page was open, newest first. */
  recent: BookCreatedDetail[];
}

const IndexStatusContext = React.createContext<IndexStatusValue>({
  pendingCount: 0,
  isPending: () => false,
  recent: [],
});

export function useIndexStatus() {
  return React.useContext(IndexStatusContext);
}

const RECENT_MAX = 5;

export function IndexStatusProvider({ children }: { children: React.ReactNode }) {
  const [pending, setPending] = React.useState<{ count: number; ids: Set<string> }>({
    count: 0,
    ids: new Set(),
  });
  const [recent, setRecent] = React.useState<BookCreatedDetail[]>([]);

  React.useEffect(() => {
    const watcher = createPendingWatcher({
      check: () => api.adminIndexStatus(),
      count: (s) => s.pending_count,
      onResult: (s) => setPending({ count: s.pending_count, ids: pendingSet(s) }),
      deps: {
        setTimeout: (fn, ms) => window.setTimeout(fn, ms),
        clearTimeout: (h) => window.clearTimeout(h as number),
        doc: document,
      },
    });
    const onCreated = (e: Event) => {
      const book = (e as CustomEvent<BookCreatedDetail>).detail;
      if (!book?.id) return;
      setRecent((cur) => [book, ...cur.filter((b) => b.id !== book.id)].slice(0, RECENT_MAX));
      // A new book is dirty until the worker embeds it; show the badge before the next poll.
      setPending((cur) =>
        cur.ids.has(book.id) ? cur : { count: cur.count + 1, ids: new Set([...cur.ids, book.id]) },
      );
      watcher.refresh();
    };
    const onRemoved = (e: Event) => {
      const ids = (e as CustomEvent<string[]>).detail;
      if (!Array.isArray(ids)) return;
      setRecent((cur) => cur.filter((book) => !ids.includes(book.id)));
      watcher.refresh();
    };
    window.addEventListener(BOOK_CREATED_EVENT, onCreated);
    window.addEventListener(BOOKS_REMOVED_EVENT, onRemoved);
    watcher.refresh();
    return () => {
      window.removeEventListener(BOOK_CREATED_EVENT, onCreated);
      window.removeEventListener(BOOKS_REMOVED_EVENT, onRemoved);
      watcher.stop();
    };
  }, []);

  const value = React.useMemo<IndexStatusValue>(
    () => ({
      pendingCount: pending.count,
      isPending: (id) => pending.ids.has(id),
      recent,
    }),
    [pending, recent],
  );

  return <IndexStatusContext.Provider value={value}>{children}</IndexStatusContext.Provider>;
}

/** Header chip — "N books indexing" while the worker backlog is non-empty. */
export function IndexingChip({ className }: { className?: string }) {
  const { pendingCount } = useIndexStatus();
  return (
    <span role="status" aria-live="polite" className={pendingCount > 0 ? className : "sr-only"}>
      {pendingCount > 0 && (
        <Badge variant="outline" data-testid="index-status-chip" data-count={pendingCount}>
          <Loader2 className="h-3 w-3 animate-spin text-primary" aria-hidden />
          {indexingLabel(pendingCount)}
        </Badge>
      )}
    </span>
  );
}

/** Per-book "Indexing…" badge, rendered only while the book id is pending. */
export function IndexingBadge({ bookId, className }: { bookId: string; className?: string }) {
  const { isPending } = useIndexStatus();
  if (!isPending(bookId)) return null;
  return (
    <Badge
      variant="outline"
      className={cn("shrink-0 text-on-surface-variant", className)}
      data-testid="indexing-badge"
      data-book-id={bookId}
      title="Generating embeddings for semantic search"
    >
      <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
      Indexing…
    </Badge>
  );
}

/** Books added via Add Book on this visit, each with its indexing badge. */
export function RecentlyAddedBooks() {
  const { recent } = useIndexStatus();
  if (recent.length === 0) return null;
  return (
    <Card data-testid="recent-books">
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Recently added</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="divide-y divide-border/60">
          {recent.map((b) => (
            <li
              key={b.id}
              className="flex items-center justify-between gap-3 py-2 text-sm"
              data-testid="recent-book"
              data-book-id={b.id}
            >
              <Link
                href={`/books/${b.id}`}
                className="min-w-0 truncate font-medium hover:underline"
              >
                {b.title}
              </Link>
              <IndexingBadge bookId={b.id} />
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
