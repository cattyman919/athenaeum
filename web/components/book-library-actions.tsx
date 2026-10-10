"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { BookmarkPlus, Check, ChevronDown, Heart, Trash2 } from "lucide-react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { StarRating } from "@/components/star-rating";
import {
  api,
  ApiError,
  isBookNotAvailable,
  type LibraryPatch,
  type ReadingStatus,
} from "@/lib/api-client";
import { apiErrorText } from "@/lib/api-errors";
import { enumLabel } from "@/lib/enum-labels";
import { cn } from "@/lib/utils";

const STAGES: ReadingStatus[] = ["want_to_read", "reading", "read"];

export interface LibraryEntryState {
  status: ReadingStatus;
  is_favorite: boolean;
  rating?: number | null;
}

/**
 * Book-detail "Add to My Library" menu (docs/07 §7 screen 3, docs/03 §6.2): pick a reading
 * stage, toggle favorite, rate, or remove. A first `reading` on a zero-copy book gets the soft
 * 409 BOOK_NOT_AVAILABLE → dialog offering the waitlist or `?force=true` (docs/07 §9).
 */
export function BookLibraryActions({
  bookId,
  entry: initial,
}: {
  bookId: string;
  entry: LibraryEntryState | null;
}) {
  const t = useTranslations("bookActions");
  const tWait = useTranslations("waitlist");
  const tEnums = useTranslations("enums");
  const tCommon = useTranslations("common");
  const router = useRouter();
  const [entry, setEntry] = React.useState<LibraryEntryState | null>(initial);
  const [open, setOpen] = React.useState(false);
  const [pending, startTransition] = React.useTransition();
  const tConfirm = useTranslations("confirm");
  const [confirmRemove, setConfirmRemove] = React.useState(false);
  const [unavailable, setUnavailable] = React.useState<string[] | null>(null);
  const rootRef = React.useRef<HTMLDivElement>(null);
  const menuId = React.useId();

  // Close the menu on outside click / Escape.
  React.useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const stageLabel = (s: string) => enumLabel(tEnums, "readingStage", s);

  function run(fn: () => Promise<void>) {
    startTransition(async () => {
      try {
        await fn();
        router.refresh();
      } catch (err) {
        if (isBookNotAvailable(err)) {
          const actions = err.body?.suggested_actions;
          setUnavailable(Array.isArray(actions) ? actions.map(String) : []);
          return;
        }
        toast.error(apiErrorText(tCommon, err, t("error")));
      }
    });
  }

  function chooseStage(status: ReadingStatus) {
    setOpen(false);
    if (entry?.status === status) return;
    run(async () => {
      if (entry) {
        const e = await api.patchLibrary(bookId, { status });
        setEntry({ ...entry, status: e.status });
        toast.success(t("updated"));
      } else {
        const e = await api.addToLibrary(bookId, status);
        setEntry({ status: e.status, is_favorite: e.is_favorite, rating: e.rating });
        toast.success(t("added"));
      }
    });
  }

  function patch(p: LibraryPatch) {
    if (!entry) return;
    run(async () => {
      const e = await api.patchLibrary(bookId, p);
      setEntry({ status: e.status, is_favorite: e.is_favorite, rating: e.rating });
      toast.success(t("updated"));
    });
  }

  function remove() {
    setOpen(false);
    run(async () => {
      await api.removeFromLibrary(bookId);
      setEntry(null);
      toast.success(t("removed"));
    });
  }

  function addAnyway() {
    setUnavailable(null);
    run(async () => {
      const e = await api.addToLibrary(bookId, "reading", { force: true });
      setEntry({ status: e.status, is_favorite: e.is_favorite, rating: e.rating });
      toast.success(t("added"));
    });
  }

  function joinWaitlist() {
    setUnavailable(null);
    startTransition(async () => {
      try {
        const w = await api.joinWaitlist(bookId);
        toast.success(tWait("joined"), {
          description: tWait("position", { position: w.position }),
        });
        router.refresh();
      } catch (err) {
        toast.error(
          err instanceof ApiError && err.status !== 429
            ? err.message
            : apiErrorText(tCommon, err, tWait("error")),
        );
      }
    });
  }

  return (
    <section
      aria-label={t("title")}
      data-testid="library-actions"
      data-status={entry?.status ?? ""}
      className="flex flex-wrap items-center gap-3"
    >
      <div ref={rootRef} className="relative">
        <Button
          variant={entry ? "outline" : "default"}
          disabled={pending}
          aria-haspopup="menu"
          aria-expanded={open}
          aria-controls={open ? menuId : undefined}
          data-testid="library-menu-trigger"
          onClick={() => setOpen((o) => !o)}
        >
          {entry ? (
            <>
              <Check className="h-4 w-4 text-primary" aria-hidden />
              {t("inLibrary")}
              <span className="text-muted-foreground" data-testid="library-menu-stage">
                · {stageLabel(entry.status)}
              </span>
            </>
          ) : (
            <>
              <BookmarkPlus className="h-4 w-4" aria-hidden />
              {t("add")}
            </>
          )}
          <ChevronDown className="h-4 w-4" aria-hidden />
        </Button>

        {open && (
          <div
            id={menuId}
            role="menu"
            aria-label={entry ? t("change") : t("add")}
            data-testid="library-menu"
            className="absolute left-0 top-full z-40 mt-2 w-60 rounded-lg border border-border bg-card p-1 shadow-lg"
          >
            <p className="px-2 py-1.5 text-xs font-medium text-muted-foreground">
              {entry ? t("change") : t("add")}
            </p>
            {STAGES.map((s) => (
              <button
                key={s}
                type="button"
                role="menuitemradio"
                aria-checked={entry?.status === s}
                data-testid={`library-stage-${s}`}
                onClick={() => chooseStage(s)}
                className="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-sm hover:bg-secondary focus-visible:bg-secondary focus-visible:outline-none"
              >
                {stageLabel(s)}
                {entry?.status === s && <Check className="h-4 w-4 text-primary" aria-hidden />}
              </button>
            ))}
            {entry && (
              <>
                <div className="my-1 h-px bg-border" role="separator" />
                <button
                  type="button"
                  role="menuitem"
                  data-testid="library-remove"
                  onClick={() => {
                    setOpen(false);
                    setConfirmRemove(true);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-destructive hover:bg-secondary focus-visible:bg-secondary focus-visible:outline-none"
                >
                  <Trash2 className="h-4 w-4" aria-hidden />
                  {t("remove")}
                </button>
              </>
            )}
          </div>
        )}
      </div>

      {entry && (
        <>
          <Button
            variant="outline"
            size="icon"
            disabled={pending}
            aria-pressed={entry.is_favorite}
            aria-label={entry.is_favorite ? t("unfavorite") : t("favorite")}
            title={entry.is_favorite ? t("unfavorite") : t("favorite")}
            data-testid="library-favorite"
            onClick={() => patch({ is_favorite: !entry.is_favorite })}
          >
            <Heart
              className={cn("h-4 w-4", entry.is_favorite && "fill-accent text-accent")}
              aria-hidden
            />
          </Button>
          <StarRating
            value={entry.rating ?? null}
            disabled={pending}
            label={t("rate")}
            testId="library-rating"
            onChange={(rating) => patch({ rating })}
          />
        </>
      )}

      <Dialog open={unavailable !== null} onOpenChange={(o) => !o && setUnavailable(null)}>
        <DialogContent className="p-6" data-testid="unavailable-dialog">
          <DialogTitle className="text-lg font-semibold">{t("unavailable.title")}</DialogTitle>
          <DialogDescription className="mt-2 text-sm text-muted-foreground">
            {t("unavailable.body")}
          </DialogDescription>
          <div className="mt-6 flex flex-wrap justify-end gap-2">
            <Button
              variant="ghost"
              onClick={() => setUnavailable(null)}
              data-testid="unavailable-dismiss"
            >
              {t("unavailable.dismiss")}
            </Button>
            <Button variant="outline" onClick={addAnyway} data-testid="unavailable-add-anyway">
              {t("unavailable.addAnyway")}
            </Button>
            {/* suggested_actions (docs/03 §1): "reserve" / "notify_when_available" both map to the
                in-app waitlist (docs/03 §6.3 — no email delivery). */}
            {(unavailable?.length === 0 ||
              unavailable?.some((a) => a === "reserve" || a === "notify_when_available")) && (
              <Button onClick={joinWaitlist} data-testid="unavailable-join-waitlist">
                {t("unavailable.joinWaitlist")}
              </Button>
            )}
          </div>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title={tConfirm("libraryRemove.title")}
        description={tConfirm("libraryRemove.description")}
        confirmLabel={tConfirm("libraryRemove.action")}
        onConfirm={remove}
      />
    </section>
  );
}
