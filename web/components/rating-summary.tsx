import { useTranslations } from "next-intl";
import { formatAvg, hasRating, type RatingSource } from "@/lib/rating";
import { cn } from "@/lib/utils";

/**
 * Read-only community rating `★ x.y (n)` (docs/03 §5.1/§5.2). Renders nothing for unrated
 * books unless `showNone` keeps a stable slot with the localized "No ratings yet".
 */
export function RatingSummary({
  book,
  showNone = false,
  className,
}: {
  book: RatingSource;
  showNone?: boolean;
  className?: string;
}) {
  const t = useTranslations();
  if (!hasRating(book)) {
    return showNone ? (
      <span
        data-testid="book-rating-none"
        className={cn("text-xs text-muted-foreground", className)}
      >
        {t("book.rating.none")}
      </span>
    ) : null;
  }
  return (
    <span
      data-testid="book-rating"
      data-avg={formatAvg(book.rating_avg)}
      data-count={book.rating_count}
      className={cn("text-xs font-medium text-accent", className)}
    >
      {t("catalog.card.rating", { avg: formatAvg(book.rating_avg), count: book.rating_count })}
    </span>
  );
}
