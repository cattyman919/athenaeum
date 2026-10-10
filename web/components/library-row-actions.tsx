"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { Heart, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { StarRating } from "@/components/star-rating";
import { api, type LibraryPatch, type ReadingStatus } from "@/lib/api-client";
import { apiErrorText } from "@/lib/api-errors";
import { enumLabel } from "@/lib/enum-labels";
import { cn } from "@/lib/utils";

const STAGES: ReadingStatus[] = ["want_to_read", "reading", "read"];

/**
 * Inline editing for a `/library` row (docs/07 §7 screen 4, docs/03 §6.2): reading stage,
 * favorite, rating and remove. Each change PATCHes/DELETEs and refreshes the server list so
 * KPI counts and stage tabs stay in sync.
 */
export function LibraryRowActions({
  bookId,
  title,
  status,
  isFavorite,
  rating,
}: {
  bookId: string;
  title: string;
  status: ReadingStatus;
  isFavorite: boolean;
  rating: number | null;
}) {
  const t = useTranslations("library");
  const tEnums = useTranslations("enums");
  const tCommon = useTranslations("common");
  const router = useRouter();
  const [pending, startTransition] = React.useTransition();
  const selectId = React.useId();

  function run(fn: () => Promise<unknown>, done: string) {
    startTransition(async () => {
      try {
        await fn();
        toast.success(done);
        router.refresh();
      } catch (err) {
        toast.error(apiErrorText(tCommon, err, t("toast.error")));
      }
    });
  }

  const patch = (p: LibraryPatch) => run(() => api.patchLibrary(bookId, p), t("toast.updated"));

  return (
    <div
      className="flex flex-wrap items-center gap-2"
      data-testid="row-actions"
      aria-busy={pending || undefined}
    >
      <label htmlFor={selectId} className="sr-only">
        {t("row.stage")} — {title}
      </label>
      <select
        id={selectId}
        value={status}
        disabled={pending}
        data-testid="row-stage-select"
        onChange={(e) => patch({ status: e.target.value as ReadingStatus })}
        className="h-8 rounded-md border border-border bg-card px-2 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
      >
        {STAGES.map((s) => (
          <option key={s} value={s}>
            {enumLabel(tEnums, "readingStage", s)}
          </option>
        ))}
      </select>
      <StarRating
        value={rating}
        disabled={pending}
        label={t("row.rating", { stars: rating ?? 0 })}
        testId="row-rating"
        onChange={(next) => patch({ rating: next })}
      />
      <Button
        variant="ghost"
        size="icon"
        disabled={pending}
        aria-pressed={isFavorite}
        aria-label={isFavorite ? t("row.unfavorite") : t("row.favorite")}
        title={isFavorite ? t("row.unfavorite") : t("row.favorite")}
        data-testid="row-favorite-toggle"
        onClick={() => patch({ is_favorite: !isFavorite })}
      >
        <Heart className={cn("h-4 w-4", isFavorite && "fill-accent text-accent")} aria-hidden />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        disabled={pending}
        aria-label={t("row.remove")}
        title={t("row.remove")}
        data-testid="row-remove"
        onClick={() => run(() => api.removeFromLibrary(bookId), t("toast.removed"))}
      >
        <Trash2 className="h-4 w-4 text-destructive" aria-hidden />
      </Button>
    </div>
  );
}
