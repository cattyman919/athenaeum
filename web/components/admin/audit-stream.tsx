"use client";

import * as React from "react";
import { ArrowRight, Pause, Radio } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api-client";
import {
  AUDIT_MAX_ROWS,
  AUDIT_POLL_MS,
  createPoller,
  mergeEvents,
  nextSince,
  statusLabel,
  statusTone,
} from "@/lib/admin";
import type { CopyEvent } from "@/lib/admin-types";
import { cn } from "@/lib/utils";

const timeFmt = (iso: string) =>
  new Date(iso).toLocaleTimeString("en-US", {
    timeZone: "UTC",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });

/**
 * RealTimeAuditStream — `copy_events` via GET /api/admin/copy-events?since=… every 3 s
 * (docs/03 §8.1; WebSockets out of scope). Polling pauses while the tab is hidden.
 */
export function RealTimeAuditStream({ initial }: { initial: CopyEvent[] }) {
  const [events, setEvents] = React.useState<CopyEvent[]>(initial);
  const [fresh, setFresh] = React.useState<Set<number>>(new Set());
  const [live, setLive] = React.useState(true);
  const sinceRef = React.useRef<string | null>(nextSince(initial, null));

  React.useEffect(() => {
    const poller = createPoller(
      async () => {
        const batch = await api.adminCopyEvents(sinceRef.current, AUDIT_MAX_ROWS);
        if (batch.length === 0) return;
        sinceRef.current = nextSince(batch, sinceRef.current);
        setEvents((cur) => mergeEvents(cur, batch));
        setFresh(new Set(batch.map((e) => e.id)));
      },
      AUDIT_POLL_MS,
      {
        setTimeout: (fn, ms) => window.setTimeout(fn, ms),
        clearTimeout: (h) => window.clearTimeout(h as number),
        doc: document,
      },
    );
    const syncLive = () => setLive(!document.hidden);
    document.addEventListener("visibilitychange", syncLive);
    poller.start();
    return () => {
      poller.stop();
      document.removeEventListener("visibilitychange", syncLive);
    };
  }, []);

  return (
    <Card data-testid="audit-stream">
      <CardHeader className="flex-row items-center justify-between space-y-0 pb-3">
        <CardTitle className="text-base">Real-time audit stream</CardTitle>
        <span
          className={cn(
            "flex items-center gap-1.5 text-xs font-medium",
            live ? "text-status-available-fg" : "text-muted-foreground",
          )}
          data-testid="audit-live"
          data-state={live ? "live" : "paused"}
        >
          {live ? (
            <Radio className="h-3.5 w-3.5 animate-pulse" aria-hidden />
          ) : (
            <Pause className="h-3.5 w-3.5" aria-hidden />
          )}
          {live ? "Live · 3 s" : "Paused"}
        </span>
      </CardHeader>
      <CardContent>
        {events.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground" data-testid="audit-empty">
            No copy events yet. Status changes and shelf moves appear here as they happen.
          </p>
        ) : (
          <ol className="divide-y divide-border/60" aria-live="polite" aria-label="Copy events">
            {events.map((e) => (
              <li
                key={e.id}
                data-testid="audit-row"
                data-event-id={e.id}
                data-barcode={e.barcode}
                data-to={e.to_status}
                data-new={fresh.has(e.id) ? "true" : undefined}
                className={cn(
                  "grid grid-cols-[auto_1fr_auto] items-center gap-x-4 gap-y-1 py-2.5 text-sm transition-colors",
                  fresh.has(e.id) && "bg-primary/5",
                )}
              >
                <time dateTime={e.created_at} className="font-mono text-xs text-muted-foreground">
                  {timeFmt(e.created_at)}
                </time>
                <div className="min-w-0">
                  <p className="truncate font-medium">{e.book_title}</p>
                  <p className="mono-label text-muted-foreground">
                    {e.barcode} · {e.source}
                    {e.actor ? ` · ${e.actor.name}` : ""}
                  </p>
                </div>
                <div className="flex items-center gap-1.5">
                  {e.from_status !== null && (
                    <>
                      <Badge variant={statusTone(e.from_status)}>
                        {statusLabel(e.from_status)}
                      </Badge>
                      <ArrowRight className="h-3.5 w-3.5 text-muted-foreground" aria-label="to" />
                    </>
                  )}
                  <Badge variant={statusTone(e.to_status)} data-testid="audit-to">
                    {statusLabel(e.to_status)}
                  </Badge>
                </div>
              </li>
            ))}
          </ol>
        )}
      </CardContent>
    </Card>
  );
}
