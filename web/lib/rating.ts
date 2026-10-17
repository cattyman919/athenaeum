// Rating-average display helpers (docs/03 §5.1/§5.2: `rating_avg` is rounded to 1 dp by the API).

export interface RatingSource {
  rating_avg?: number | null;
  rating_count?: number | null;
}

/** True when a book has at least one rating; unrated books render nothing. */
export function hasRating(src: RatingSource): src is { rating_avg: number; rating_count: number } {
  return (
    typeof src.rating_avg === "number" &&
    typeof src.rating_count === "number" &&
    src.rating_count > 0
  );
}

/** "4.6" — always one decimal. */
export function formatAvg(avg: number): string {
  return avg.toFixed(1);
}
