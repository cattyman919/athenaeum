import { BookCopy, Library, MessagesSquare, Ruler, Users } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import type { AdminKpis } from "@/lib/admin-types";
import { pct } from "@/lib/admin";

const fmt = (n: number) => n.toLocaleString("en-US");

interface Kpi {
  key: string;
  label: string;
  value: string;
  detail: React.ReactNode;
  icon: React.ComponentType<{ className?: string }>;
  meter?: number;
}

/** Executive KPI grid — GET /api/admin/data (docs/03 §8.1, docs/07 screen 6a). */
export function KpiGrid({ data }: { data: AdminKpis }) {
  const { copies, shelf_capacity: cap, chat_week: chat } = data;
  const kpis: Kpi[] = [
    {
      key: "titles",
      label: "Titles in catalog",
      value: fmt(data.titles_in_catalog),
      detail: "Distinct bibliographic records",
      icon: Library,
    },
    {
      key: "copies",
      label: "Physical copies",
      value: fmt(copies.total),
      detail: (
        <span className="flex flex-wrap gap-x-3 gap-y-1">
          <span data-testid="kpi-copies-available">
            <b className="font-mono text-status-available-fg">{fmt(copies.available)}</b> available
          </span>
          <span data-testid="kpi-copies-checked-out">
            <b className="font-mono text-status-out-fg">{fmt(copies.checked_out)}</b> checked out
          </span>
          <span data-testid="kpi-copies-missing">
            <b className="font-mono text-status-na-fg">{fmt(copies.missing_lost)}</b> missing
          </span>
        </span>
      ),
      icon: BookCopy,
      meter: pct(copies.available, copies.total),
    },
    {
      key: "capacity",
      label: "Shelf capacity",
      value: `${pct(cap.occupied, cap.total)}%`,
      detail: (
        <span data-testid="kpi-capacity-slots">
          <b className="font-mono">{fmt(cap.occupied)}</b> / {fmt(cap.total)} slots occupied
        </span>
      ),
      icon: Ruler,
      meter: pct(cap.occupied, cap.total),
    },
    {
      key: "patrons",
      label: "Active patrons",
      value: fmt(data.active_patrons),
      detail: "Patron accounts with activity",
      icon: Users,
    },
    {
      key: "chat",
      label: "AI Librarian · 7 days",
      value: fmt(chat.conversations),
      detail: (
        <span data-testid="kpi-chat-messages">
          conversations · <b className="font-mono">{fmt(chat.messages)}</b> messages
        </span>
      ),
      icon: MessagesSquare,
    },
  ];

  return (
    <section aria-label="Key metrics" className="space-y-2">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5" data-testid="kpi-grid">
        {kpis.map(({ key, label, value, detail, icon: Icon, meter }) => (
          <Card key={key} data-testid="kpi-card" data-kpi={key}>
            <CardContent className="flex h-full flex-col gap-2 p-5">
              <div className="flex items-center justify-between text-xs font-medium uppercase tracking-wider text-on-surface-variant">
                <span>{label}</span>
                <Icon className="h-4 w-4 text-primary" aria-hidden />
              </div>
              <p className="font-mono text-3xl font-semibold tabular-nums" data-testid="kpi-value">
                {value}
              </p>
              {meter !== undefined && (
                <div
                  className="h-1.5 w-full overflow-hidden rounded-full bg-surface-high"
                  role="meter"
                  aria-label={`${label} ratio`}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={meter}
                >
                  <div className="h-full rounded-full bg-primary" style={{ width: `${meter}%` }} />
                </div>
              )}
              <div className="mt-auto text-xs text-muted-foreground">{detail}</div>
            </CardContent>
          </Card>
        ))}
      </div>
      <p className="text-right text-[11px] text-muted-foreground">
        Computed{" "}
        <time dateTime={data.computed_at}>
          {new Date(data.computed_at).toLocaleString("en-US", {
            timeZone: "UTC",
            timeZoneName: "short",
          })}
        </time>
      </p>
    </section>
  );
}
