"use client";

// Patron Lookup tab (docs/11 §2a/§2b, §7–§8; docs/07 §Scanner dialog). Every decode — camera
// or manual — is pre-checked locally (EAN-13 checksum / ATH tag shape) and resolved through
// POST /api/books/scan (docs/03 §5.4). A returnable copy offers POST /api/copies/{id}/return
// (§6.4). Localized via `scanner.*` (docs/14 §1).

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import {
  ArrowRight,
  BookOpen,
  Check,
  Hash,
  Loader2,
  MapPin,
  RotateCcw,
  SearchX,
  ShieldCheck,
  Undo2,
} from "lucide-react";
import { AvailabilityBadge } from "@/components/availability-badge";
import { BookCover } from "@/components/book-cover";
import { scanFeedback } from "@/components/isbn-scanner/isbn-scanner";
import { WaitlistCta } from "@/components/waitlist-cta";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { api } from "@/lib/api-client";
import { apiErrorText } from "@/lib/api-errors";
import { acceptedCode, classifyScanCode, type RejectedScan } from "@/lib/barcode";
import { enumLabel } from "@/lib/enum-labels";
import { applyConflict, applyReturn, needsWaitlist, scanErrorView } from "@/lib/scan";
import type { ScanLocation, ScanResponse } from "@/lib/types";
import { cn } from "@/lib/utils";
import { ladderSteps, sectionLabel } from "@/lib/wayfinding";
import { CameraViewfinder, type ViewfinderFlash } from "./camera-viewfinder";

export interface PatronLookupPanelProps {
  /** Camera runs only while the tab is visible. */
  active: boolean;
  /** Called before following a link out of the dialog (closes it). */
  onNavigate: () => void;
  /** Admins only: hands an unknown ISBN to Staff Intake. */
  onStaffIntake?: (isbn: string) => void;
}

const COPY_STATUS_VARIANT: Record<string, "available" | "out" | "unavailable"> = {
  available: "available",
  checked_out: "out",
  reserved_waitlist: "out",
  missing_lost: "unavailable",
};

export function PatronLookupPanel({ active, onNavigate, onStaffIntake }: PatronLookupPanelProps) {
  const t = useTranslations("scanner");
  const tCommon = useTranslations("common");
  const tEnums = useTranslations("enums");
  const router = useRouter();
  const inputRef = React.useRef<HTMLInputElement>(null);

  const [manual, setManual] = React.useState("");
  const [hint, setHint] = React.useState<string | null>(null);
  const [pendingCode, setPendingCode] = React.useState<string | null>(null);
  const [result, setResult] = React.useState<ScanResponse | null>(null);
  const [returning, setReturning] = React.useState(false);
  const [flash, setFlash] = React.useState<ViewfinderFlash>(null);

  // Guards against duplicate lookups while a request is in flight or the same code is shown.
  const busy = React.useRef<string | null>(null);
  const shown = React.useRef<string | null>(null);

  React.useEffect(() => {
    if (!flash) return;
    const id = setTimeout(() => setFlash(null), 600);
    return () => clearTimeout(id);
  }, [flash]);

  const reject = React.useCallback(
    (verdict: RejectedScan) => {
      setHint(t(`reject.${verdict.kind}`));
      setFlash("reject");
    },
    [t],
  );

  const lookup = React.useCallback(
    async (raw: string, source: "camera" | "manual") => {
      const verdict = classifyScanCode(raw);
      const code = acceptedCode(verdict);
      if (code === null) {
        reject(verdict as RejectedScan);
        return;
      }
      if (busy.current === code) return;
      if (source === "camera" && shown.current === code) return;
      busy.current = code;
      setHint(null);
      setPendingCode(code);
      try {
        const res = await api.scanCode(code);
        shown.current = code;
        setResult(res);
        setFlash("ok");
        if (source === "camera") scanFeedback();
      } catch (err) {
        const view = scanErrorView(err);
        if (view.kind === "invalid_isbn") {
          setHint(t("errors.invalidIsbn"));
          setFlash("reject");
          toast.error(t("errors.invalidIsbn"));
        } else {
          toast.error(apiErrorText(tCommon, err, t("errors.lookup")));
        }
      } finally {
        busy.current = null;
        setPendingCode(null);
      }
    },
    [reject, t, tCommon],
  );

  const scanNext = React.useCallback(() => {
    shown.current = null;
    setResult(null);
    setHint(null);
    setManual("");
    inputRef.current?.focus();
  }, []);

  async function returnCopy() {
    if (!result?.copy) return;
    const copy = result.copy;
    setReturning(true);
    try {
      const ret = await api.returnCopy(copy.id);
      setResult((r) => (r ? applyReturn(r, ret) : r));
      toast.success(
        ret.hold
          ? t("return.held", { barcode: copy.barcode })
          : t("return.success", { barcode: copy.barcode }),
      );
      router.refresh();
    } catch (err) {
      const view = scanErrorView(err);
      if (view.kind === "conflict") {
        const status = view.currentStatus
          ? enumLabel(tEnums, "copyStatus", view.currentStatus)
          : "—";
        toast.error(t("return.conflict", { status }));
        setResult((r) => (r ? applyConflict(r, view.currentStatus) : r));
      } else if (view.kind === "not_found") {
        toast.error(t("return.notFound"));
        scanNext();
      } else {
        toast.error(apiErrorText(tCommon, err, t("return.error")));
      }
    } finally {
      setReturning(false);
    }
  }

  return (
    <div className="grid min-h-[540px] grid-cols-1 bg-surface-low lg:grid-cols-12">
      <div className="flex flex-col justify-between gap-4 bg-card p-4 sm:p-6 lg:col-span-7">
        <CameraViewfinder
          active={active && !returning}
          onDetected={(code) => void lookup(code, "camera")}
          onRejected={reject}
          onUseManual={() => inputRef.current?.focus()}
          flash={flash}
        />

        <form
          className="flex flex-col gap-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            void lookup(manual, "manual");
          }}
        >
          <div className="flex items-center justify-between gap-2">
            <label htmlFor="scanner-manual" className="text-xs font-medium text-on-surface-variant">
              {t("manual.label")}
            </label>
            <span className="font-mono text-[11px] text-outline">{t("manual.hint")}</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <Hash
                className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-outline"
                aria-hidden
              />
              <input
                ref={inputRef}
                id="scanner-manual"
                data-testid="scanner-manual-input"
                autoComplete="off"
                spellCheck={false}
                placeholder={t("manual.placeholder")}
                value={manual}
                onChange={(e) => setManual(e.target.value)}
                aria-invalid={Boolean(hint)}
                aria-describedby={hint ? "scanner-manual-hint" : undefined}
                className="h-10 w-full rounded-md bg-surface-low pl-9 pr-3 font-mono text-sm shadow-inner focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>
            <Button
              type="submit"
              variant="secondary"
              disabled={pendingCode !== null}
              data-testid="scanner-manual-submit"
            >
              {pendingCode !== null ? (
                <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
              ) : (
                <ShieldCheck className="h-4 w-4" aria-hidden />
              )}
              {t("manual.submit")}
            </Button>
          </div>
          {hint && (
            <p
              id="scanner-manual-hint"
              role="alert"
              className="text-sm text-destructive"
              data-testid="scanner-manual-error"
            >
              {hint}
            </p>
          )}
        </form>
      </div>

      <div className="flex flex-col justify-between gap-4 p-4 sm:p-6 lg:col-span-5">
        <div className="flex flex-col gap-4">
          <StatusPill pendingCode={pendingCode} result={result} />
          {result && (
            <ResultCard
              result={result}
              returning={returning}
              onReturn={() => void returnCopy()}
              onStaffIntake={onStaffIntake}
            />
          )}
        </div>
        {result && (
          <div className="flex flex-col gap-2 sm:flex-row">
            {result.book && (
              <Link
                href={`/books/${result.book.id}`}
                onClick={onNavigate}
                data-testid="scan-view-record"
                className={cn(buttonVariants(), "flex-1")}
              >
                {t("result.viewRecord")}
                <ArrowRight className="h-4 w-4" aria-hidden />
              </Link>
            )}
            <Button variant="secondary" onClick={scanNext} data-testid="scan-next">
              <RotateCcw className="h-4 w-4" aria-hidden />
              {t("result.scanNext")}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

function ResultCard({
  result,
  returning,
  onReturn,
  onStaffIntake,
}: {
  result: ScanResponse;
  returning: boolean;
  onReturn: () => void;
  onStaffIntake?: (isbn: string) => void;
}) {
  const t = useTranslations("scanner");
  const tEnums = useTranslations("enums");
  const tWay = useTranslations("wayfinding");

  if (!result.in_catalog || result.match === "none" || !result.book) {
    return (
      <div
        className="flex flex-col items-start gap-3 rounded-lg bg-card p-4 shadow-sm"
        data-testid="scan-not-in-catalog"
      >
        <span className="flex items-center gap-2 font-semibold">
          <SearchX className="h-4 w-4 text-outline" aria-hidden />
          {t("notInCatalog.title")}
        </span>
        <p className="text-sm text-on-surface-variant">
          {t("notInCatalog.body", { code: result.code })}
        </p>
        {onStaffIntake && result.code_type === "isbn13" && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onStaffIntake(result.code)}
            data-testid="scan-staff-intake"
          >
            {t("notInCatalog.staffCta")}
          </Button>
        )}
      </div>
    );
  }

  const { book, copy, availability } = result;
  const isCopy = result.match === "copy" && copy;

  return (
    <div
      className="flex flex-col gap-3 rounded-lg bg-card p-4 shadow-sm"
      data-testid="scan-result"
      data-match={result.match}
    >
      <span className="flex items-center gap-1.5 text-xs font-medium text-primary">
        <BookOpen className="h-4 w-4" aria-hidden />
        {isCopy ? t("result.copyMatched") : t("result.bookMatched")}
      </span>

      <div className="flex items-start gap-4">
        <div className="w-24 shrink-0 shadow-md">
          <BookCover
            src={book.cover_url}
            alt={book.title}
            title={book.title}
            accent={book.section?.color}
          />
        </div>
        <div className="flex min-w-0 flex-col gap-0.5">
          <h3 className="text-lg font-semibold leading-snug" data-testid="scan-result-title">
            {book.title}
          </h3>
          <p className="text-sm font-medium">{book.authors.join(", ")}</p>
          {book.isbn13 && (
            <p className="font-mono text-xs text-outline">
              {t("result.isbn", { isbn: book.isbn13 })}
            </p>
          )}
          {book.section && (
            <span className="mt-1 flex items-center gap-1.5 text-xs text-on-surface-variant">
              <span
                className="h-2 w-2 rounded-full"
                style={{ backgroundColor: book.section.color }}
                aria-hidden
              />
              {sectionLabel(tWay, book.section.code, book.section.name)}
            </span>
          )}
        </div>
      </div>

      {isCopy && (
        <dl
          className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded bg-surface-low p-3 text-sm"
          data-testid="scan-copy"
          data-status={copy.status}
        >
          <dt className="text-on-surface-variant">{t("result.barcode")}</dt>
          <dd className="font-mono font-bold text-primary">{copy.barcode}</dd>
          <dt className="text-on-surface-variant">{t("result.status")}</dt>
          <dd>
            <Badge
              variant={COPY_STATUS_VARIANT[copy.status] ?? "unavailable"}
              data-testid="scan-copy-status"
              data-status={copy.status}
            >
              {enumLabel(tEnums, "copyStatus", copy.status)}
            </Badge>
          </dd>
          <dt className="text-on-surface-variant">{t("result.condition")}</dt>
          <dd className="font-mono">{copy.condition}</dd>
        </dl>
      )}

      {availability && (
        <div
          className="flex flex-wrap items-center justify-between gap-2"
          data-testid="scan-availability"
          data-available={availability.available_copies}
        >
          <AvailabilityBadge state={availability.state} />
          <span className="text-right">
            <span className="block font-mono text-xs font-bold text-status-available-fg">
              {t("result.copiesOnShelf", { count: availability.available_copies })}
            </span>
            <span className="block text-[11px] text-outline">
              {t("result.ofTotal", {
                available: availability.available_copies,
                total: availability.total_copies,
              })}
            </span>
          </span>
        </div>
      )}

      <Ladder location={result.wayfinding} />

      {isCopy &&
        (result.returnable ? (
          <Button onClick={onReturn} disabled={returning} data-testid="scan-return">
            {returning ? (
              <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
            ) : (
              <Undo2 className="h-4 w-4" aria-hidden />
            )}
            {returning ? t("result.returning") : t("result.returnCopy")}
          </Button>
        ) : (
          <p className="text-xs text-on-surface-variant" data-testid="scan-not-returnable">
            {t("result.notReturnable")}
          </p>
        ))}

      {!isCopy && needsWaitlist(result) && (
        <div
          className="flex flex-col gap-2 rounded bg-status-out-bg p-3 text-status-out-fg"
          data-testid="scan-waitlist"
        >
          <p className="text-sm font-medium">{t("result.waitlistTitle")}</p>
          <WaitlistCta bookId={book.id} hold={null} />
        </div>
      )}
    </div>
  );
}

function StatusPill({
  pendingCode,
  result,
}: {
  pendingCode: string | null;
  result: ScanResponse | null;
}) {
  const t = useTranslations("scanner");
  const decoded = !pendingCode && result;
  return (
    <div
      className="flex items-center gap-3 rounded-lg bg-card p-4 shadow-sm"
      role="status"
      aria-live="polite"
      data-testid="scanner-status"
    >
      <span
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-full",
          decoded ? "bg-status-available-bg text-status-available-fg" : "bg-surface-container",
        )}
      >
        {pendingCode ? (
          <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
        ) : decoded ? (
          <Check className="h-4 w-4" aria-hidden />
        ) : (
          <BookOpen className="h-4 w-4 text-outline" aria-hidden />
        )}
      </span>
      <div className="min-w-0">
        {pendingCode ? (
          <p className="text-sm">{t("status.pending", { code: pendingCode })}</p>
        ) : decoded ? (
          <>
            <p className="font-mono text-[11px] font-bold uppercase text-status-available-fg">
              {result.code_type === "copy_barcode" ? t("status.copy") : t("status.isbn")}
            </p>
            <p className="truncate font-mono text-xs font-bold">{result.code}</p>
          </>
        ) : (
          <p className="text-sm text-on-surface-variant">{t("status.idle")}</p>
        )}
      </div>
    </div>
  );
}

function Ladder({ location }: { location: ScanLocation | null }) {
  const t = useTranslations("scanner");
  const tWay = useTranslations("wayfinding");
  return (
    <div className="flex flex-col gap-1.5 rounded bg-surface-low p-3" data-testid="scan-ladder">
      <span className="flex items-center gap-1 text-xs font-medium text-on-surface-variant">
        <MapPin className="h-4 w-4" aria-hidden />
        {t("result.location")}
      </span>
      {location ? (
        <>
          <span className="font-mono text-xs text-primary" data-testid="scan-path">
            {location.path}
          </span>
          <ol className="flex flex-col gap-1">
            {(location.parts ? ladderSteps(tWay, location.parts) : location.ladder_steps).map(
              (step, i) => (
                <li key={i} className="flex items-start gap-2 text-sm">
                  <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-secondary font-mono text-[10px]">
                    {i + 1}
                  </span>
                  <span>{step}</span>
                </li>
              ),
            )}
          </ol>
        </>
      ) : (
        <span className="text-sm text-on-surface-variant">{t("result.noLocation")}</span>
      )}
    </div>
  );
}
