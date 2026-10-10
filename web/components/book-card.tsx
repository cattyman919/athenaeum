import Link from "next/link";
import { useTranslations } from "next-intl";
import { AvailabilityBadge } from "@/components/availability-badge";
import { BookCover } from "@/components/book-cover";
import type { BookSummary } from "@/lib/types";

/** 2:3 grid card with a top-right availability beacon and a mono call-number chip (docs/12 §9). */
export function BookCard({ book }: { book: BookSummary }) {
  const t = useTranslations("catalog.card");
  const { total_copies: total, available_copies: available } = book.availability;
  return (
    <Link
      href={`/books/${book.id}`}
      aria-label={t("openBook", { title: book.title })}
      data-testid="book-card"
      data-title={book.title}
      className="group flex flex-col gap-3 rounded-lg border border-border bg-card p-3 shadow-sm transition-all duration-150 hover:border-primary/40 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <div className="relative">
        <BookCover
          src={book.cover_url}
          alt={t("coverAlt", { title: book.title })}
          title={book.title}
          accent={book.section?.color}
        />
        <AvailabilityBadge
          state={book.availability.state}
          className="absolute right-2 top-2 shadow-sm"
        />
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        <h3 className="line-clamp-1 text-lg font-semibold leading-tight transition-colors group-hover:text-primary">
          {book.title}
        </h3>
        <p className="line-clamp-1 text-xs font-medium text-muted-foreground">
          {book.authors.join(", ")}
        </p>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          {book.location && (
            <span
              className="mono-label rounded bg-secondary px-1.5 py-0.5 text-muted-foreground"
              title={t("shelf")}
            >
              {book.location.path}
            </span>
          )}
          <span className="text-xs text-muted-foreground">
            {total === 0 ? t("noCopies") : t("copies", { available, total })}
          </span>
        </div>
      </div>
    </Link>
  );
}
