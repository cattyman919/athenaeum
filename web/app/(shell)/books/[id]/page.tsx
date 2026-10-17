import Link from "next/link";
import { notFound } from "next/navigation";
import { getTranslations } from "next-intl/server";
import { BookOpen, ChevronRight, Sparkles } from "lucide-react";
import { AvailabilityBanner } from "@/components/availability-banner";
import { BookCover } from "@/components/book-cover";
import { RatingSummary } from "@/components/rating-summary";
import { BookLibraryActions } from "@/components/book-library-actions";
import { WaitlistCta, type HoldState } from "@/components/waitlist-cta";
import { BookLocationCard } from "@/components/book-location-card";
import { CopyManifest } from "@/components/copy-manifest";
import { NeighborCarousel } from "@/components/neighbor-carousel";
import { buttonVariants } from "@/components/ui/button";
import { apiServer } from "@/lib/api-server";
import type { BookDetail, WaitlistItem } from "@/lib/types";
import { cn } from "@/lib/utils";
import { sectionLabel } from "@/lib/wayfinding";

async function loadBook(id: string): Promise<BookDetail> {
  const res = await apiServer(`/api/books/${encodeURIComponent(id)}`);
  if (res.status === 404) notFound();
  if (!res.ok) throw new Error(`book detail failed: ${res.status}`);
  return (await res.json()) as BookDetail;
}

/** The caller's active hold on this book, from GET /api/waitlist (docs/03 §6.3). */
async function loadHold(id: string): Promise<HoldState | null> {
  try {
    const res = await apiServer("/api/waitlist");
    if (!res.ok) return null;
    const { items } = (await res.json()) as { items: WaitlistItem[] };
    const h = items.find((w) => w.book.id === id);
    return h ? { status: h.status, position: h.position, expires_at: h.expires_at } : null;
  } catch {
    return null;
  }
}

// Screen 3 — Stitch 723960ca… Book Detail (docs/07 §7, docs/05 §5, docs/12 screen 3).
export default async function BookPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const [book, t, tRecord, hold] = await Promise.all([
    loadBook(id),
    getTranslations("book"),
    getTranslations("book.record"),
    loadHold(id),
  ]);
  const none = tRecord("none");
  // Section chip/breadcrumb in the active locale (docs/14 §6.1); raw name is the fallback.
  const tWay = await getTranslations("wayfinding");
  const sectionName = book.section ? sectionLabel(tWay, book.section.code, book.section.name) : "";

  const record: Array<[string, string | number | null]> = [
    [tRecord("isbn13"), book.isbn13],
    [tRecord("isbn10"), book.isbn10],
    [tRecord("dewey"), book.dewey_num],
    [tRecord("loc"), book.loc_num],
    [tRecord("publisher"), book.publisher],
    [tRecord("published"), book.published_year],
  ];

  return (
    <article className="flex flex-col gap-10" data-testid="book-detail">
      <nav
        aria-label="Breadcrumb"
        className="flex items-center gap-1 text-xs text-muted-foreground"
      >
        <Link href="/catalog" className="hover:text-foreground">
          {t("breadcrumb.catalog")}
        </Link>
        {book.section && (
          <>
            <ChevronRight className="h-3 w-3" aria-hidden />
            <Link href={`/catalog?section=${book.section.code}`} className="hover:text-foreground">
              {sectionName}
            </Link>
          </>
        )}
        <ChevronRight className="h-3 w-3" aria-hidden />
        <span className="font-medium text-foreground">{book.title}</span>
      </nav>

      <div className="grid gap-8 lg:grid-cols-[2fr_3fr]">
        <aside className="flex flex-col gap-6">
          <div className="mx-auto w-full max-w-xs">
            <BookCover
              src={book.cover_url}
              alt={book.title}
              title={book.title}
              accent={book.section?.color}
              className="shadow-lg"
            />
          </div>
          <Link
            href={`/chat?prefill=${encodeURIComponent(book.title)}`}
            data-testid="ask-ai"
            className={cn(
              buttonVariants({ variant: "outline" }),
              "border-accent/50 text-foreground hover:bg-accent/10",
            )}
          >
            <Sparkles className="h-4 w-4 text-accent" aria-hidden />
            {t("askAi")}
          </Link>
          <section
            aria-labelledby="record-title"
            className="rounded-lg border border-border bg-card p-4"
          >
            <h2 id="record-title" className="mb-3 text-lg font-semibold">
              {tRecord("title")}
            </h2>
            <dl
              className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm"
              data-testid="catalog-record"
            >
              {record.map(([label, value]) => (
                <div key={label} className="contents">
                  <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
                  <dd className="mono-label normal-case tracking-normal">{value ?? none}</dd>
                </div>
              ))}
            </dl>
          </section>
        </aside>

        <div className="flex min-w-0 flex-col gap-6">
          <header className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              {book.section && (
                <span
                  className="inline-flex items-center gap-2 rounded-full border border-border px-3 py-1 text-xs font-medium"
                  data-testid="book-section"
                >
                  <span
                    className="h-2 w-2 rounded-full"
                    style={{ backgroundColor: book.section.color }}
                    aria-hidden
                  />
                  {sectionName}
                </span>
              )}
              {book.published_year && (
                <span className="mono-label text-muted-foreground">{book.published_year}</span>
              )}
            </div>
            <h1 className="text-3xl font-bold tracking-tight" data-testid="book-title">
              {book.title}
            </h1>
            {book.subtitle && <p className="text-lg text-muted-foreground">{book.subtitle}</p>}
            <p className="text-sm text-muted-foreground">
              {t("by", { authors: book.authors.join(", ") })}
            </p>
            <RatingSummary book={book} showNone className="text-sm" />
          </header>

          <BookLibraryActions bookId={book.id} entry={book.my_library_entry} />

          <AvailabilityBanner availability={book.availability}>
            {book.availability.available_copies === 0 && (
              <WaitlistCta
                key={hold ? `${hold.status}:${hold.position}` : "none"}
                bookId={book.id}
                hold={hold}
              />
            )}
          </AvailabilityBanner>

          <BookLocationCard wayfinding={book.wayfinding} />

          {book.description && (
            <section aria-labelledby="synopsis-title">
              <h2 id="synopsis-title" className="mb-2 text-lg font-semibold">
                {t("synopsis")}
              </h2>
              <p className="text-sm leading-relaxed">{book.description}</p>
            </section>
          )}

          <CopyManifest copies={book.copies} />

          {book.isbns && book.isbns.length > 1 && (
            <section aria-labelledby="editions-title" data-testid="book-editions">
              <h2 id="editions-title" className="mb-2 text-lg font-semibold">
                {t("editions.title")}
              </h2>
              <ul className="flex flex-col gap-1">
                {book.isbns.map((entry) => (
                  <li
                    key={entry.isbn13}
                    className="flex items-center gap-2 font-mono text-sm"
                    data-testid="edition-entry"
                  >
                    <BookOpen className="h-3.5 w-3.5 shrink-0 text-outline" aria-hidden />
                    <span>{entry.isbn13}</span>
                    {entry.is_primary && (
                      <span className="rounded-full bg-primary/10 px-2 py-0.5 font-sans text-xs font-medium text-primary">
                        {t("editions.primary")}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      </div>

      <NeighborCarousel neighbors={book.neighbors} />
    </article>
  );
}
