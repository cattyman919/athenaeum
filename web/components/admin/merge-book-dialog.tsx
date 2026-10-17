"use client";

// Admin "Merge into…" dialog (plan §4, docs/03 §8.6). Lets an admin pick a target book via
// catalog search and merge the source book into it. English-only (docs/14 §1).

import * as React from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { GitMerge, Loader2, Search } from "lucide-react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { api, ApiError } from "@/lib/api-client";
import type { AdminBookItem } from "@/lib/admin-types";

interface MergeBookDialogProps {
  /** The book to be merged away (source). */
  source: Pick<AdminBookItem, "id" | "title" | "isbn13">;
  onMerged?: () => void;
}

export function MergeBookDialog({ source, onMerged }: MergeBookDialogProps) {
  const router = useRouter();
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [results, setResults] = React.useState<AdminBookItem[]>([]);
  const [searching, setSearching] = React.useState(false);
  const [target, setTarget] = React.useState<AdminBookItem | null>(null);
  const [confirmOpen, setConfirmOpen] = React.useState(false);

  // Reset state when dialog opens/closes.
  React.useEffect(() => {
    if (!open) {
      setQuery("");
      setResults([]);
      setTarget(null);
    }
  }, [open]);

  // Debounced search using the admin books endpoint; excludes the source book.
  const searchTimer = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  function handleQueryChange(q: string) {
    setQuery(q);
    setTarget(null);
    if (searchTimer.current) clearTimeout(searchTimer.current);
    if (!q.trim()) {
      setResults([]);
      return;
    }
    searchTimer.current = setTimeout(() => {
      setSearching(true);
      api
        .adminBooks({ search: q, per_page: 10 })
        .then((res) => setResults(res.items.filter((b) => b.id !== source.id)))
        .catch(() => setResults([]))
        .finally(() => setSearching(false));
    }, 300);
  }

  async function doMerge() {
    if (!target) return;
    try {
      const res = await api.mergeBooks(source.id, target.id);
      toast.success(
        `Merged "${source.title}" into "${target.title}" — ${res.merged_copies} copies transferred, ${res.isbns.length} ISBNs combined.`,
        { action: { label: "View", onClick: () => router.push(`/books/${target.id}`) } },
      );
      setOpen(false);
      onMerged?.();
      router.refresh();
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Merge failed. Please try again.";
      toast.error(msg);
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
            data-testid="book-merge"
          >
            <GitMerge className="h-3.5 w-3.5" aria-hidden />
            Merge into…
          </Button>
        </DialogTrigger>
        <DialogContent className="max-w-lg">
          <div className="border-b border-border p-5">
            <DialogTitle className="flex items-center gap-2 text-base font-semibold">
              <GitMerge className="h-4 w-4 text-primary" aria-hidden />
              Merge &ldquo;{source.title}&rdquo;
            </DialogTitle>
            <DialogDescription className="mt-1.5 text-sm text-muted-foreground">
              Search for the canonical book to keep. All copies, ISBNs, waitlists and reading lists
              will be moved; the source record is deleted.
            </DialogDescription>
          </div>

          <div className="space-y-4 p-5">
            <div className="relative">
              <Search
                className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-outline"
                aria-hidden
              />
              <Input
                placeholder="Search catalog for the target book…"
                value={query}
                onChange={(e) => handleQueryChange(e.target.value)}
                className="pl-9"
                data-testid="merge-search"
              />
            </div>

            {searching && (
              <p className="flex items-center gap-2 text-sm text-on-surface-variant">
                <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
                Searching…
              </p>
            )}

            {results.length > 0 && (
              <ul
                className="divide-y divide-border rounded-lg border border-border text-sm"
                data-testid="merge-results"
              >
                {results.map((b) => (
                  <li key={b.id}>
                    <button
                      type="button"
                      className={`w-full px-3 py-2.5 text-left transition-colors hover:bg-surface-low focus:outline-none focus:ring-2 focus:ring-inset focus:ring-ring ${
                        target?.id === b.id ? "bg-surface-container font-medium" : ""
                      }`}
                      onClick={() => setTarget(b)}
                      data-testid={`merge-pick-${b.id}`}
                    >
                      <p className="truncate font-medium">{b.title}</p>
                      <p className="mt-0.5 truncate text-xs text-on-surface-variant">
                        {b.authors.join(", ")}
                        {b.isbn13 ? ` · ${b.isbn13}` : ""}
                        {` · ${b.availability.total_copies} copies`}
                      </p>
                    </button>
                  </li>
                ))}
              </ul>
            )}

            {!searching && query && results.length === 0 && (
              <p className="text-sm text-on-surface-variant">No matching books found.</p>
            )}

            {target && (
              <div
                className="rounded-md bg-surface-container px-3 py-2 text-sm"
                data-testid="merge-selected"
              >
                <span className="text-on-surface-variant">Merging into: </span>
                <span className="font-medium">{target.title}</span>
              </div>
            )}

            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button
                size="sm"
                disabled={!target}
                onClick={() => setConfirmOpen(true)}
                data-testid="merge-confirm-open"
              >
                <GitMerge className="h-3.5 w-3.5" aria-hidden />
                Merge
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {target && (
        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title={`Merge "${source.title}" into "${target.title}"?`}
          description={`This will move all copies, ISBNs and patron data to "${target.title}" and permanently delete the source record. This cannot be undone.`}
          confirmLabel="Merge permanently"
          onConfirm={() => void doMerge()}
          destructive
        />
      )}
    </>
  );
}
