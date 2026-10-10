"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Command } from "cmdk";
import { Search } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { AvailabilityBadge } from "@/components/availability-badge";
import { BookCover } from "@/components/book-cover";
import { api } from "@/lib/api-client";
import type { BookSummary, SearchResponse } from "@/lib/types";

export const OPEN_PALETTE_EVENT = "athenaeum:open-palette";

/** ⌘K quick search: client-only, calls the same endpoint as /catalog (docs/07 §7). */
export function CommandPalette() {
  const t = useTranslations("palette");
  const router = useRouter();
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [items, setItems] = React.useState<BookSummary[]>([]);
  const [loading, setLoading] = React.useState(false);

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    const onOpen = () => setOpen(true);
    window.addEventListener("keydown", onKey);
    window.addEventListener(OPEN_PALETTE_EVENT, onOpen);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener(OPEN_PALETTE_EVENT, onOpen);
    };
  }, []);

  React.useEffect(() => {
    const q = query.trim();
    if (!open || q === "") {
      setItems([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      api
        .json<SearchResponse>(`/api/books/search?${new URLSearchParams({ q, per_page: "6" })}`, {
          signal: ctrl.signal,
        })
        .then((r) => setItems(r.items))
        .catch(() => undefined)
        .finally(() => !ctrl.signal.aborted && setLoading(false));
    }, 200);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [query, open]);

  const go = (path: string) => {
    setOpen(false);
    setQuery("");
    router.push(path);
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent aria-describedby="palette-desc" data-testid="command-palette" className="p-0">
        <DialogTitle className="sr-only">{t("title")}</DialogTitle>
        <DialogDescription id="palette-desc" className="sr-only">
          {t("description")}
        </DialogDescription>
        <Command shouldFilter={false} label={t("title")}>
          <div className="flex items-center gap-2 border-b border-border px-4">
            <Search className="h-4 w-4 text-outline" aria-hidden />
            <Command.Input
              value={query}
              onValueChange={setQuery}
              placeholder={t("placeholder")}
              data-testid="palette-input"
              className="h-12 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
          </div>
          <Command.List className="max-h-80 overflow-y-auto p-2" data-testid="palette-list">
            {query.trim() === "" && (
              <p className="px-3 py-6 text-center text-sm text-muted-foreground">{t("hint")}</p>
            )}
            {query.trim() !== "" && loading && items.length === 0 && (
              <p className="px-3 py-6 text-center text-sm text-muted-foreground">
                {t("searching")}
              </p>
            )}
            {query.trim() !== "" && !loading && items.length === 0 && (
              <p className="px-3 py-6 text-center text-sm text-muted-foreground">{t("empty")}</p>
            )}
            {items.map((b) => (
              <Command.Item
                key={b.id}
                value={b.id}
                onSelect={() => go(`/books/${b.id}`)}
                data-testid="palette-item"
                className="flex cursor-pointer items-center justify-between gap-3 rounded-md px-3 py-2 text-sm data-[selected=true]:bg-secondary"
              >
                <span className="flex min-w-0 items-center gap-3">
                  <BookCover src={b.cover_url} alt="" title={b.title} className="w-8 shrink-0" />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate font-medium">{b.title}</span>
                    <span className="truncate text-xs text-muted-foreground">
                      {b.authors.join(", ")}
                    </span>
                  </span>
                </span>
                <AvailabilityBadge state={b.availability.state} />
              </Command.Item>
            ))}
            {query.trim() !== "" && (
              <Command.Item
                value="__all__"
                onSelect={() => go(`/catalog?${new URLSearchParams({ q: query.trim() })}`)}
                data-testid="palette-see-all"
                className="mt-1 cursor-pointer rounded-md px-3 py-2 text-sm text-primary data-[selected=true]:bg-secondary"
              >
                {t("seeAll", { query: query.trim() })}
              </Command.Item>
            )}
          </Command.List>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
