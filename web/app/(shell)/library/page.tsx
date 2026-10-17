import { Suspense } from "react";
import Link from "next/link";
import { getFormatter, getTranslations } from "next-intl/server";
import { BookOpen, Sparkles } from "lucide-react";
import { ScanToReturnButton } from "@/components/barcode-scanner";
import { BookCover } from "@/components/book-cover";
import { LeaveWaitlistButton } from "@/components/leave-waitlist-button";
import { LibraryRowActions } from "@/components/library-row-actions";
import { RetryButton } from "@/components/retry-button";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { LoansList } from "@/components/loans-list";
import { apiServer } from "@/lib/api-server";
import { enumLabel } from "@/lib/enum-labels";
import { sortLoans } from "@/lib/loans";
import type { LibraryResponse, LoansResponse, WaitlistItem } from "@/lib/types";
import { cn } from "@/lib/utils";
import { shelfLine } from "@/lib/wayfinding";

const STAGES = ["all", "reading", "read", "want_to_read", "favorites", "holds", "loans"] as const;
type Stage = (typeof STAGES)[number];

async function getJson<T>(path: string): Promise<T | null> {
  try {
    const res = await apiServer(path);
    return res.ok ? ((await res.json()) as T) : null;
  } catch {
    return null;
  }
}

// Screen 4 — Stitch 9cee… My Library (docs/07 §7, docs/12 screen 4).
export default async function LibraryPage({
  searchParams,
}: {
  searchParams: Promise<{ stage?: string }>;
}) {
  const sp = await searchParams;
  const stage: Stage = (STAGES as readonly string[]).includes(sp.stage ?? "")
    ? (sp.stage as Stage)
    : "all";
  const t = await getTranslations("library");

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-3xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
        </div>
        <ScanToReturnButton />
      </header>

      {/* In-page Suspense (not route-level loading.tsx — docs/07 §9, Next 15.5 note). */}
      <Suspense key={stage} fallback={<LibrarySkeleton />}>
        <LibraryContent stage={stage} />
      </Suspense>

      <aside className="flex items-center justify-between gap-4 rounded-lg border border-accent/40 bg-accent/5 p-4">
        <div className="flex items-start gap-3">
          <Sparkles className="mt-0.5 h-5 w-5 text-accent" aria-hidden />
          <div>
            <p className="font-semibold">{t("ai.title")}</p>
            <p className="text-xs text-muted-foreground">{t("ai.body")}</p>
          </div>
        </div>
        <Link
          href={`/chat?prefill=${encodeURIComponent(t("ai.prefill"))}`}
          data-testid="ai-banner-link"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          {t("ai.cta")}
        </Link>
      </aside>
    </div>
  );
}

function LibrarySkeleton() {
  return (
    <div className="flex flex-col gap-8" aria-busy="true" data-testid="library-skeleton">
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-[84px] w-full" />
        ))}
      </div>
      <Skeleton className="h-9 w-full max-w-2xl" />
      <div className="flex flex-col gap-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-20 w-full" />
        ))}
      </div>
    </div>
  );
}

async function LibraryContent({ stage }: { stage: Stage }) {
  const [t, tEnums, tWay, format] = await Promise.all([
    getTranslations("library"),
    getTranslations("enums"),
    getTranslations("wayfinding"),
    getFormatter(),
  ]);

  const query = new URLSearchParams({ per_page: "50" });
  if (stage !== "all" && stage !== "holds" && stage !== "loans") query.set("status", stage);
  const [lib, holds, loansData] = await Promise.all([
    getJson<LibraryResponse>(`/api/library?${query}`),
    getJson<{ items: WaitlistItem[] }>("/api/waitlist"),
    getJson<LoansResponse>("/api/loans"),
  ]);
  if (!lib || (stage === "holds" && !holds) || (stage === "loans" && !loansData)) {
    return (
      <div
        role="alert"
        data-testid="library-error"
        className="flex flex-col items-start gap-3 rounded-lg border border-border bg-card p-6 shadow-sm"
      >
        <p className="font-semibold">{t("error.title")}</p>
        <p className="text-sm text-muted-foreground">{t("error.body")}</p>
        <RetryButton testId="library-retry" />
      </div>
    );
  }
  const counts = lib.counts ?? { total: 0, reading: 0, read: 0, want_to_read: 0, favorites: 0 };
  const holdItems = holds?.items ?? [];
  const loanItems = loansData?.items ?? [];
  const activeLoansCount = loanItems.filter((l) => l.kind === "active").length;

  const kpis: Array<[string, number, string]> = [
    ["total", counts.total, t("kpi.total")],
    ["reading", counts.reading, t("kpi.reading")],
    ["read", counts.read, t("kpi.read")],
    ["favorites", counts.favorites, t("kpi.favorites")],
  ];
  const tabCount: Record<Stage, number> = {
    all: counts.total,
    reading: counts.reading,
    read: counts.read,
    want_to_read: counts.want_to_read,
    favorites: counts.favorites,
    holds: holdItems.length,
    loans: activeLoansCount,
  };

  return (
    <div className="flex flex-col gap-8">
      <section className="grid grid-cols-2 gap-4 md:grid-cols-4" aria-label="KPI">
        {kpis.map(([key, value, label]) => (
          <div
            key={key}
            data-testid={`kpi-${key}`}
            className="rounded-lg border border-border bg-card p-4 shadow-sm"
          >
            <p className="text-3xl font-bold tracking-tight" data-testid="kpi-value">
              {value}
            </p>
            <p className="text-xs font-medium text-muted-foreground">{label}</p>
          </div>
        ))}
      </section>

      <nav
        aria-label={t("tabs.label")}
        className="flex flex-wrap gap-2 border-b border-border pb-2"
      >
        {STAGES.map((s) => (
          <Link
            key={s}
            href={s === "all" ? "/library" : `/library?stage=${s}`}
            prefetch={false}
            data-testid={`tab-${s}`}
            aria-current={stage === s ? "page" : undefined}
            className={cn(
              "rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground",
              stage === s && "bg-secondary text-foreground",
            )}
          >
            {t(`tabs.${s}`)} <span className="mono-label ml-1">{tabCount[s]}</span>
          </Link>
        ))}
      </nav>

      {stage === "loans" ? (
        loanItems.length === 0 ? (
          <Empty title={t("loans.empty")} body={t("loans.scanHint")} />
        ) : (
          <LoansList loans={sortLoans(loanItems)} />
        )
      ) : stage === "holds" ? (
        holdItems.length === 0 ? (
          <Empty title={t("empty.holds")} body={t("empty.holdsBody")} cta={t("empty.browse")} />
        ) : (
          <ul className="flex flex-col gap-3" data-testid="holds-list">
            {holdItems.map((h) => (
              <li
                key={h.id}
                data-testid="hold-row"
                className="flex items-center gap-4 rounded-lg border border-border bg-card p-4"
              >
                <div className="w-12 shrink-0">
                  <BookCover src={h.book.cover_url} alt={h.book.title} title={h.book.title} />
                </div>
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <Link
                    href={`/books/${h.book.id}`}
                    className="truncate font-semibold hover:text-primary"
                  >
                    {h.book.title}
                  </Link>
                  <p className="text-xs text-muted-foreground">{h.book.authors.join(", ")}</p>
                  <div className="flex items-center gap-2 text-xs">
                    <Badge variant={h.status === "ready" ? "available" : "out"}>
                      {h.status === "ready"
                        ? t("holds.ready", {
                            date: format.dateTime(new Date(h.expires_at ?? Date.now()), {
                              dateStyle: "medium",
                            }),
                          })
                        : t("holds.waiting")}
                    </Badge>
                    <span data-testid="hold-position" className="text-muted-foreground">
                      {t("holds.position", { position: h.position })}
                    </span>
                  </div>
                </div>
                <LeaveWaitlistButton bookId={h.book.id} />
              </li>
            ))}
          </ul>
        )
      ) : lib.items.length === 0 ? (
        <Empty title={t("empty.title")} body={t("empty.body")} cta={t("empty.browse")} />
      ) : (
        <ul className="flex flex-col gap-3" data-testid="library-list">
          {lib.items.map((it) => (
            <li
              key={it.book.id}
              data-testid="library-row"
              data-title={it.book.title}
              className="flex flex-wrap items-center gap-4 rounded-lg border border-border bg-card p-4 md:flex-nowrap"
            >
              <div className="w-12 shrink-0">
                <BookCover src={it.book.cover_url} alt={it.book.title} title={it.book.title} />
              </div>
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <Link
                  href={`/books/${it.book.id}`}
                  className="truncate font-semibold hover:text-primary"
                >
                  {it.book.title}
                </Link>
                <p className="text-xs text-muted-foreground">{it.book.authors.join(", ")}</p>
                <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                  <Badge variant="outline" data-testid="row-stage">
                    {enumLabel(tEnums, "readingStage", it.status)}
                  </Badge>
                  {it.location ? (
                    <span className="mono-label" data-testid="row-shelf">
                      {shelfLine(tWay, {
                        floor_number: it.location.floor,
                        shelf_code: it.location.shelf_code,
                      })}
                    </span>
                  ) : (
                    <span>{t("row.noShelf")}</span>
                  )}
                  {it.status === "reading" && it.progress_pct !== null && (
                    <span data-testid="row-progress">
                      {t("row.progress", { pct: it.progress_pct })}
                    </span>
                  )}
                  {it.is_favorite && (
                    <span className="font-medium text-accent" data-testid="row-favorite">
                      ★ {enumLabel(tEnums, "readingStage", "favorites")}
                    </span>
                  )}
                </div>
              </div>
              <LibraryRowActions
                bookId={it.book.id}
                title={it.book.title}
                status={it.status}
                isFavorite={it.is_favorite}
                rating={it.rating}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Empty({ title, body, cta }: { title: string; body: string; cta?: string }) {
  return (
    <div
      data-testid="library-empty"
      className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-card px-6 py-12 text-center"
    >
      <BookOpen className="h-6 w-6 text-outline" aria-hidden />
      <p className="font-semibold">{title}</p>
      <p className="max-w-sm text-sm text-muted-foreground">{body}</p>
      {cta && (
        <Link href="/catalog" className={buttonVariants({ size: "sm" })}>
          {cta}
        </Link>
      )}
    </div>
  );
}
