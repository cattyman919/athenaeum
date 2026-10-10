"use client";

import * as React from "react";
import Link from "next/link";
import { Loader2, MapPin, ScanLine } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { normalizeBarcode, occupancyLevel, statusLabel } from "@/lib/admin";
import { COPY_STATUSES, type CopyStatus, type ShelfInspectorData } from "@/lib/admin-types";
import { cn } from "@/lib/utils";

const selectClass =
  "h-8 rounded-md border border-border bg-card px-2 text-xs shadow-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50";

const SLOT_FILL: Record<string, string> = {
  low: "bg-status-available-dot",
  mid: "bg-primary",
  high: "bg-status-out-dot",
  full: "bg-status-na-dot",
  empty: "bg-surface-highest",
};

interface Props {
  shelf: ShelfInspectorData | null;
  loading: boolean;
  /** Resolves to an error message, or null on success. */
  onAssign: (barcode: string) => Promise<string | null>;
  onStatus: (copyId: string, status: CopyStatus) => Promise<void>;
  onSelectShelf: (id: string) => void;
}

/** Shelf manifest inspector: occupancy meter, copy slots, quick-assign (docs/07 screen 6b). */
export function ShelfInspector({ shelf, loading, onAssign, onStatus, onSelectShelf }: Props) {
  const [barcode, setBarcode] = React.useState("");
  const [assignError, setAssignError] = React.useState<string | null>(null);
  const [assigning, setAssigning] = React.useState(false);
  const [busyCopy, setBusyCopy] = React.useState<string | null>(null);

  React.useEffect(() => {
    setAssignError(null);
    setBarcode("");
  }, [shelf?.id]);

  if (!shelf) {
    return (
      <Card data-testid="shelf-inspector" data-state={loading ? "loading" : "empty"}>
        <CardContent className="p-6">
          {loading ? (
            <div className="space-y-3">
              <Skeleton className="h-6 w-2/3" />
              <Skeleton className="h-3 w-full" />
              <Skeleton className="h-24 w-full" />
            </div>
          ) : (
            <div className="flex flex-col items-center gap-2 py-10 text-center text-sm text-muted-foreground">
              <MapPin className="h-6 w-6" aria-hidden />
              Select a shelf in the tree or on the map to inspect its manifest.
            </div>
          )}
        </CardContent>
      </Card>
    );
  }

  const level = occupancyLevel(shelf.occupancy);
  const slots = Math.max(shelf.occupancy.total, shelf.occupancy.occupied);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const code = normalizeBarcode(barcode);
    if (!code) {
      setAssignError("Enter or scan a copy barcode.");
      return;
    }
    if (shelf!.copies.some((c) => c.barcode === code)) {
      setAssignError(`${code} is already on this shelf.`);
      return;
    }
    setAssigning(true);
    const err = await onAssign(code);
    setAssigning(false);
    setAssignError(err);
    if (!err) setBarcode("");
  }

  async function changeStatus(copyId: string, status: CopyStatus) {
    setBusyCopy(copyId);
    try {
      await onStatus(copyId, status);
    } finally {
      setBusyCopy(null);
    }
  }

  return (
    <Card
      data-testid="shelf-inspector"
      data-state="ready"
      data-shelf-id={shelf.id}
      aria-busy={loading}
    >
      <CardHeader className="space-y-2 pb-4">
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="text-base">Shelf manifest</CardTitle>
          <Badge variant="outline" className="gap-1.5">
            <span
              className="h-2 w-2 rounded-full"
              style={{ backgroundColor: shelf.section.color }}
              aria-hidden
            />
            {shelf.section.code} · {shelf.section.name}
          </Badge>
        </div>
        <p className="font-mono text-sm" data-testid="inspector-path">
          {shelf.path}
        </p>
        <p className="text-xs text-muted-foreground">
          Floor {shelf.floor} · Aisle {shelf.aisle} · Shelf {shelf.code} · Row {shelf.row_level},{" "}
          {shelf.height_tier}
        </p>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="space-y-2">
          <div className="flex items-baseline justify-between text-xs">
            <span className="font-medium uppercase tracking-wider text-on-surface-variant">
              Slot occupancy
            </span>
            <span className="font-mono tabular-nums" data-testid="inspector-occupancy">
              {shelf.occupancy.occupied}/{shelf.occupancy.total}
            </span>
          </div>
          <div
            className="grid gap-1"
            style={{ gridTemplateColumns: `repeat(${Math.max(slots, 1)}, minmax(0, 1fr))` }}
            role="meter"
            aria-label="Slot occupancy"
            aria-valuemin={0}
            aria-valuemax={shelf.occupancy.total}
            aria-valuenow={shelf.occupancy.occupied}
          >
            {Array.from({ length: slots }, (_, i) => (
              <span
                key={i}
                className={cn(
                  "h-3 rounded-sm",
                  i < shelf.occupancy.occupied ? SLOT_FILL[level] : "bg-surface-high",
                )}
              />
            ))}
          </div>
        </div>

        <form onSubmit={submit} className="space-y-1.5" data-testid="quick-assign">
          <Label htmlFor="quick-assign-input" className="flex items-center gap-1.5 text-xs">
            <ScanLine className="h-3.5 w-3.5" aria-hidden />
            Quick-assign copy to this shelf
          </Label>
          <div className="flex gap-2">
            <Input
              id="quick-assign-input"
              data-testid="quick-assign-input"
              placeholder="ATH-002941-C02"
              autoComplete="off"
              spellCheck={false}
              className="h-9 font-mono"
              value={barcode}
              onChange={(e) => setBarcode(e.target.value)}
              aria-invalid={assignError ? true : undefined}
              aria-describedby={assignError ? "quick-assign-error" : undefined}
            />
            <Button
              type="submit"
              size="sm"
              className="h-9"
              disabled={assigning}
              data-testid="quick-assign-submit"
            >
              {assigning && <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />}
              Assign
            </Button>
          </div>
          {assignError && (
            <p
              id="quick-assign-error"
              className="text-xs text-destructive"
              role="alert"
              data-testid="quick-assign-error"
            >
              {assignError}
            </p>
          )}
        </form>

        <div>
          <p className="mb-2 text-xs font-medium uppercase tracking-wider text-on-surface-variant">
            Copies on shelf ({shelf.copies.length})
          </p>
          {shelf.copies.length === 0 ? (
            <p className="rounded-md bg-surface-low px-3 py-4 text-center text-xs text-muted-foreground">
              This shelf is empty.
            </p>
          ) : (
            <ul className="divide-y divide-border/60 rounded-md border border-border">
              {shelf.copies.map((c) => (
                <li
                  key={c.id}
                  className="flex items-center gap-3 px-3 py-2"
                  data-testid="inspector-copy"
                  data-barcode={c.barcode}
                  data-status={c.status}
                >
                  <div className="min-w-0 flex-1">
                    <Link
                      href={`/books/${c.book_id}`}
                      className="block truncate text-sm font-medium hover:underline"
                    >
                      {c.title}
                    </Link>
                    <span className="mono-label text-muted-foreground">{c.barcode}</span>
                  </div>
                  <select
                    aria-label={`Status of ${c.barcode}`}
                    data-testid="copy-status-select"
                    className={selectClass}
                    value={c.status}
                    disabled={busyCopy === c.id}
                    onChange={(e) => void changeStatus(c.id, e.target.value as CopyStatus)}
                  >
                    {COPY_STATUSES.map((s) => (
                      <option key={s} value={s}>
                        {statusLabel(s)}
                      </option>
                    ))}
                  </select>
                </li>
              ))}
            </ul>
          )}
        </div>

        {shelf.adjacent.length > 0 && (
          <div>
            <p className="mb-2 text-xs font-medium uppercase tracking-wider text-on-surface-variant">
              Adjacent shelves
            </p>
            <div className="flex flex-wrap gap-2">
              {shelf.adjacent.map((a) => (
                <button
                  key={a.id}
                  type="button"
                  onClick={() => onSelectShelf(a.id)}
                  className="rounded-md border border-border px-2 py-1 font-mono text-xs hover:bg-surface-low"
                  data-testid="adjacent-shelf"
                  title={a.path}
                >
                  {a.code} · {a.occupancy.occupied}/{a.occupancy.total}
                </button>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
