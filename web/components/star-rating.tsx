"use client";

import { useTranslations } from "next-intl";
import { Star } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * Five-star rating control (docs/07 §8 rating). Clicking the active star clears the
 * rating (`null`), matching PATCH /api/library semantics (docs/03 §6.2).
 */
export function StarRating({
  value,
  onChange,
  disabled,
  label,
  testId = "star-rating",
}: {
  value: number | null;
  onChange: (next: number | null) => void;
  disabled?: boolean;
  label: string;
  testId?: string;
}) {
  const t = useTranslations("library.row");
  return (
    <div role="group" aria-label={label} className="flex items-center" data-testid={testId}>
      {[1, 2, 3, 4, 5].map((n) => {
        const filled = value !== null && n <= value;
        return (
          <button
            key={n}
            type="button"
            disabled={disabled}
            aria-label={t("rate", { stars: n })}
            aria-pressed={value === n}
            data-testid={`${testId}-${n}`}
            onClick={() => onChange(value === n ? null : n)}
            className="rounded p-0.5 text-muted-foreground transition-colors hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
          >
            <Star className={cn("h-4 w-4", filled && "fill-accent text-accent")} aria-hidden />
          </button>
        );
      })}
    </div>
  );
}
