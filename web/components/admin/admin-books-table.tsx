"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Loader2, Search } from "lucide-react";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api, ApiError } from "@/lib/api-client";
import type { AdminBookItem } from "@/lib/admin-types";
import { BOOK_CREATED_EVENT, BOOKS_REMOVED_EVENT } from "@/lib/index-status";
import { MergeBookDialog } from "./merge-book-dialog";

export function AdminBooksTable() {
  const router = useRouter();
  const [query, setQuery] = React.useState("");
  const [page, setPage] = React.useState(1);
  const [books, setBooks] = React.useState<AdminBookItem[]>([]);
  const [total, setTotal] = React.useState(0);
  const [pages, setPages] = React.useState(0);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [actionError, setActionError] = React.useState("");
  const [selected, setSelected] = React.useState<string[]>([]);
  const [deleting, setDeleting] = React.useState<AdminBookItem[] | null>(null);
  const [revision, setRevision] = React.useState(0);
  const request = React.useRef(0);
  const firstLoad = React.useRef(true);
  const selectAll = React.useRef<HTMLInputElement>(null);
  const opener = React.useRef<HTMLElement | null>(null);
  const searchInput = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    const requestRef = request;
    const id = ++requestRef.current;
    const delay = firstLoad.current ? 0 : 250;
    firstLoad.current = false;
    const timer = setTimeout(() => {
      setLoading(true);
      setError("");
      api
        .adminBooks({ search: query.trim(), page, per_page: 20 })
        .then((res) => {
          if (id !== request.current) return;
          if (page > 1 && page > Math.max(1, res.meta.total_pages)) {
            setBooks([]);
            setSelected([]);
            setPage(Math.max(1, res.meta.total_pages));
            return;
          }
          setSelected((current) =>
            current.filter((selectedId) => res.items.some((book) => book.id === selectedId)),
          );
          setBooks(res.items);
          setTotal(res.meta.total);
          setPages(res.meta.total_pages);
        })
        .catch(() => {
          if (id === request.current) setError("Could not load books. Please try again.");
        })
        .finally(() => {
          if (id === request.current) setLoading(false);
        });
    }, delay);
    return () => {
      clearTimeout(timer);
      requestRef.current++;
    };
  }, [query, page, revision]);

  React.useEffect(() => {
    const onCreated = () => {
      request.current++;
      setLoading(true);
      setSelected([]);
      setRevision((n) => n + 1);
    };
    window.addEventListener(BOOK_CREATED_EVENT, onCreated);
    return () => window.removeEventListener(BOOK_CREATED_EVENT, onCreated);
  }, []);

  React.useEffect(() => {
    if (selectAll.current)
      selectAll.current.indeterminate = selected.length > 0 && selected.length < books.length;
  }, [selected, books]);

  function changeQuery(value: string) {
    request.current++;
    setLoading(true);
    setQuery(value);
    setPage(1);
    setSelected([]);
    setActionError("");
  }

  function changePage(next: number) {
    request.current++;
    setLoading(true);
    setPage(next);
    setSelected([]);
    setActionError("");
  }

  function reload() {
    request.current++;
    setLoading(true);
    setRevision((n) => n + 1);
  }

  async function removeBooks() {
    if (!deleting || deleting.some((book) => !book.removable || book.active_loans > 0)) return;
    try {
      if (deleting.length === 1) await api.deleteAdminBook(deleting[0].id);
      else await api.bulkDeleteAdminBooks(deleting.map((book) => book.id));
      window.dispatchEvent(
        new CustomEvent(BOOKS_REMOVED_EVENT, { detail: deleting.map((book) => book.id) }),
      );
      setSelected([]);
      setActionError("");
      toast.success(
        deleting.length === 1
          ? `Removed “${deleting[0].title}”.`
          : `Removed ${deleting.length} books.`,
      );
      reload();
      router.refresh();
    } catch (err) {
      const blocked =
        err instanceof ApiError && Array.isArray(err.body?.blocked)
          ? err.body.blocked.filter(
              (item): item is { title: string; active_loans: number } =>
                typeof item === "object" &&
                item !== null &&
                typeof item.title === "string" &&
                typeof item.active_loans === "number",
            )
          : [];
      setActionError(
        blocked.length
          ? `Removal blocked: ${blocked.map((item) => `${item.title} (${item.active_loans} active loans)`).join(", ")}. Books with active loans, checked-out or reserved copies cannot be removed.`
          : "Could not remove books. Please try again.",
      );
      reload();
    }
  }

  const blocked = deleting?.filter((book) => !book.removable || book.active_loans > 0) ?? [];
  const allSelected = books.length > 0 && books.every((book) => selected.includes(book.id));

  return (
    <Card data-testid="admin-books-table">
      <CardHeader className="flex-row items-baseline justify-between space-y-0 pb-3">
        <CardTitle className="text-base">Books</CardTitle>
        {!loading && !error && (
          <span className="text-xs text-muted-foreground">{total} results</span>
        )}
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="relative">
          <Search
            className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-outline"
            aria-hidden
          />
          <Input
            ref={searchInput}
            aria-label="Search books by title, author or ISBN"
            placeholder="Search title, author or ISBN…"
            value={query}
            onChange={(e) => changeQuery(e.target.value)}
            className="pl-9 pr-24"
            data-testid="admin-books-search"
          />
          {query && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="absolute right-1 top-1/2 -translate-y-1/2"
              onClick={() => changeQuery("")}
              aria-label="Clear search"
              data-testid="admin-books-clear-search"
            >
              Clear
            </Button>
          )}
        </div>
        {actionError && (
          <p role="alert" className="text-sm text-destructive">
            {actionError}
          </p>
        )}
        {loading && (
          <p role="status" className="flex items-center gap-2 text-sm text-on-surface-variant">
            <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            Loading books…
          </p>
        )}
        {!loading && error && (
          <div
            role="alert"
            data-testid="admin-books-error"
            className="flex flex-wrap items-center gap-2 text-sm text-destructive"
          >
            {error}
            <Button variant="outline" size="sm" onClick={reload}>
              Retry
            </Button>
          </div>
        )}
        {!loading && !error && books.length > 0 && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <label className="flex items-center gap-2 text-sm">
                <input
                  ref={selectAll}
                  data-testid="admin-books-select-all"
                  type="checkbox"
                  checked={allSelected}
                  onChange={(e) => setSelected(e.target.checked ? books.map((b) => b.id) : [])}
                />{" "}
                Select current page
              </label>
              <div data-testid="admin-books-bulk-bar" className="flex flex-wrap items-center gap-2">
                <span className="text-sm">{selected.length} selected</span>
                <Button
                  variant="destructive"
                  size="sm"
                  data-testid="admin-books-remove-selected"
                  disabled={selected.length === 0}
                  onClick={(event) => {
                    opener.current = event.currentTarget;
                    setDeleting(books.filter((b) => selected.includes(b.id)));
                  }}
                >
                  Remove selected ({selected.length})
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  data-testid="admin-books-clear-selection"
                  disabled={selected.length === 0}
                  onClick={() => setSelected([])}
                >
                  Clear selection
                </Button>
              </div>
            </div>
            <ul className="divide-y divide-border/60 rounded-lg border border-border text-sm">
              {books.map((b) => (
                <li
                  key={b.id}
                  className="flex flex-wrap items-center gap-3 px-3 py-2"
                  data-testid="admin-book-row"
                  data-book-id={b.id}
                >
                  <input
                    type="checkbox"
                    data-testid="admin-book-select"
                    aria-label={`Select ${b.title}`}
                    checked={selected.includes(b.id)}
                    onChange={(e) =>
                      setSelected((current) =>
                        e.target.checked ? [...current, b.id] : current.filter((id) => id !== b.id),
                      )
                    }
                  />
                  <div className="min-w-0 flex-1 basis-40">
                    <Link
                      href={`/books/${b.id}`}
                      className="block truncate font-medium hover:underline"
                    >
                      {b.title}
                    </Link>
                    <p className="text-xs text-on-surface-variant">
                      {b.authors.join(", ")}
                      {b.isbn13 ? ` · ${b.isbn13}` : ""}
                      {` · ${b.availability.total_copies} copies`}
                    </p>
                    {!b.removable && (
                      <p className="text-xs text-destructive">
                        Cannot remove: active loans, checked-out or reserved copies (
                        {b.active_loans} active loans).
                      </p>
                    )}
                  </div>
                  <div className="flex items-center gap-1">
                    <MergeBookDialog
                      source={b}
                      onMerged={() => {
                        setSelected([]);
                        reload();
                      }}
                    />
                    <Button
                      variant="outline"
                      size="sm"
                      data-testid="admin-book-remove"
                      aria-label={`Remove ${b.title}`}
                      onClick={(event) => {
                        opener.current = event.currentTarget;
                        setDeleting([b]);
                      }}
                    >
                      Remove
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          </>
        )}
        {!loading && !error && books.length === 0 && (
          <p className="text-sm text-on-surface-variant">
            {query.trim() ? `No books match “${query.trim()}”.` : "No books in the catalog."}
          </p>
        )}
        {!loading && !error && pages > 1 && (
          <nav aria-label="Books pagination" className="flex items-center justify-between gap-2">
            <Button
              variant="outline"
              size="sm"
              data-testid="admin-books-page-prev"
              disabled={page <= 1}
              onClick={() => changePage(page - 1)}
            >
              Previous
            </Button>
            <span className="text-sm">
              Page {page} of {pages}
            </span>
            <Button
              variant="outline"
              size="sm"
              data-testid="admin-books-page-next"
              disabled={page >= pages}
              onClick={() => changePage(page + 1)}
            >
              Next
            </Button>
          </nav>
        )}
      </CardContent>
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        title={
          deleting?.length === 1
            ? `Remove “${deleting[0].title}”?`
            : `Remove ${deleting?.length ?? 0} books?`
        }
        description={`This permanently deletes the selected books, their copies, ISBNs, historical loan records, waitlists, saved-library entries, ratings, embeddings and copy audit history. This cannot be undone.${blocked.length ? ` Cannot remove: ${blocked.map((b) => `${b.title} (${b.active_loans} active loans)`).join(", ")}. Active loans and checked-out or reserved copies must be cleared first.` : ""}`}
        details={
          deleting && (
            <div>
              <ul className="space-y-2" aria-label="Books selected for removal">
                {deleting.slice(0, 5).map((book) => (
                  <li key={book.id}>
                    <span className="font-medium">{book.title}</span>
                    <span className="block text-muted-foreground">
                      {book.authors.length ? book.authors.join(", ") : "Unknown author"} · ISBN:{" "}
                      {book.isbn13 ?? "Not available"} · {book.availability.total_copies} copies ·{" "}
                      {book.active_loans} active loans
                    </span>
                  </li>
                ))}
              </ul>
              {deleting.length > 5 && <p className="mt-2">+{deleting.length - 5} more</p>}
            </div>
          )
        }
        testId="remove-book-confirm"
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          const target = opener.current;
          if (target?.isConnected && !target.matches(":disabled")) target.focus();
          else searchInput.current?.focus();
        }}
        confirmLabel="Remove permanently"
        confirmDisabled={blocked.length > 0}
        onConfirm={removeBooks}
      />
    </Card>
  );
}
