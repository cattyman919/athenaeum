"use client";

// Admin books table: searchable list of all cataloged books with per-book Merge action
// (docs/03 §8.6, plan §4). English-only (docs/14 §1).

import * as React from "react";
import Link from "next/link";
import { Loader2, Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api-client";
import type { AdminBookItem } from "@/lib/admin-types";
import { MergeBookDialog } from "./merge-book-dialog";

export function AdminBooksTable() {
  const [query, setQuery] = React.useState("");
  const [books, setBooks] = React.useState<AdminBookItem[]>([]);
  const [total, setTotal] = React.useState(0);
  const [loading, setLoading] = React.useState(false);
  const [searched, setSearched] = React.useState(false);
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  function search(q: string) {
    setQuery(q);
    if (timer.current) clearTimeout(timer.current);
    const trimmed = q.trim();
    if (!trimmed) {
      setBooks([]);
      setSearched(false);
      return;
    }
    timer.current = setTimeout(() => {
      setLoading(true);
      api
        .adminBooks({ search: trimmed, per_page: 20 })
        .then((res) => {
          setBooks(res.items);
          setTotal(res.meta.total);
          setSearched(true);
        })
        .catch(() => {
          setBooks([]);
          setSearched(true);
        })
        .finally(() => setLoading(false));
    }, 300);
  }

  function onMerged(bookId: string) {
    setBooks((cur) => cur.filter((b) => b.id !== bookId));
  }

  return (
    <Card data-testid="admin-books-table">
      <CardHeader className="flex-row items-baseline justify-between space-y-0 pb-3">
        <CardTitle className="text-base">Books</CardTitle>
        {searched && <span className="text-xs text-muted-foreground">{total} results</span>}
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="relative">
          <Search
            className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-outline"
            aria-hidden
          />
          <Input
            placeholder="Search books to merge duplicates…"
            value={query}
            onChange={(e) => search(e.target.value)}
            className="pl-9"
            data-testid="admin-books-search"
          />
        </div>

        {loading && (
          <p className="flex items-center gap-2 text-sm text-on-surface-variant">
            <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            Searching…
          </p>
        )}

        {books.length > 0 && (
          <ul className="divide-y divide-border/60 rounded-lg border border-border text-sm">
            {books.map((b) => (
              <li
                key={b.id}
                className="flex items-center justify-between gap-3 px-3 py-2"
                data-testid="admin-book-row"
                data-book-id={b.id}
              >
                <div className="min-w-0 flex-1">
                  <Link
                    href={`/books/${b.id}`}
                    className="block truncate font-medium hover:underline"
                  >
                    {b.title}
                  </Link>
                  <p className="truncate text-xs text-on-surface-variant">
                    {b.authors.join(", ")}
                    {b.isbn13 ? ` · ${b.isbn13}` : ""}
                    {` · ${b.availability.total_copies} copies`}
                  </p>
                </div>
                <MergeBookDialog source={b} onMerged={() => onMerged(b.id)} />
              </li>
            ))}
          </ul>
        )}

        {searched && !loading && books.length === 0 && (
          <p className="text-sm text-on-surface-variant">No books match &ldquo;{query}&rdquo;.</p>
        )}

        {!searched && !loading && (
          <p className="text-xs text-on-surface-variant">
            Search by title, author or ISBN to find duplicate entries to merge.
          </p>
        )}
      </CardContent>
    </Card>
  );
}
