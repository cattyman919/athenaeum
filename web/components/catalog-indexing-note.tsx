"use client";

// Subtle "still indexing" note on /catalog semantic/hybrid results (docs/07 §7 screen 2,
// docs/06 §2). Hydrates with the server's `meta.index_pending`, re-checks with a cheap
// keyword search (no embedding call) every 4 s while books are pending, and refreshes the
// results once the backlog clears. Non-blocking: role=status, no focus steal.

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Loader2 } from "lucide-react";
import { api } from "@/lib/api-client";
import { createPendingWatcher } from "@/lib/index-status";

const RECHECK = new URLSearchParams({ mode: "keyword", per_page: "1" });

export function CatalogIndexingNote({ initial }: { initial: number }) {
  const t = useTranslations("catalog");
  const router = useRouter();
  const [count, setCount] = React.useState(initial);
  const last = React.useRef(initial);

  React.useEffect(() => {
    const watcher = createPendingWatcher({
      check: () => api.searchBooks(RECHECK),
      count: (r) => r.meta.index_pending ?? 0,
      onResult: (r) => {
        const next = r.meta.index_pending ?? 0;
        if (last.current > 0 && next === 0) router.refresh();
        last.current = next;
        setCount(next);
      },
      deps: {
        setTimeout: (fn, ms) => window.setTimeout(fn, ms),
        clearTimeout: (h) => window.clearTimeout(h as number),
        doc: document,
      },
    });
    watcher.refresh();
    return () => watcher.stop();
  }, [router]);

  return (
    <div role="status" aria-live="polite" className={count > 0 ? undefined : "sr-only"}>
      {count > 0 && (
        <p
          data-testid="catalog-indexing-note"
          data-count={count}
          className="flex items-center gap-2 rounded-md bg-surface-low px-3 py-2 text-xs text-muted-foreground"
        >
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" aria-hidden />
          {t("indexingNote", { count })}
        </p>
      )}
    </div>
  );
}
