"use client";

// Admin Add-Book flow (docs/11 §2c/§6, docs/07 screen 6a): Manual / ISBN / Scan tabs.
// ISBN + Scan call POST /api/books/fetch-isbn and prefill the Manual form; the cataloger
// confirms and the copy step posts POST /api/admin/books. Hosted by the /admin AddBookDialog
// and the scanner's Staff Intake tab. Admin-only → English (docs/14 §1).

import * as React from "react";
import dynamic from "next/dynamic";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Keyboard, Loader2, ScanLine, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api } from "@/lib/api-client";
import { classifyBarcode } from "@/lib/barcode";
import { BOOK_CREATED_EVENT, type BookCreatedDetail } from "@/lib/index-status";
import {
  EMPTY_FORM,
  SECTIONS,
  buildCreateBody,
  describeIntakeError,
  isbnProblem,
  metadataToForm,
  type BookForm,
  type FieldErrors,
} from "@/lib/intake";
import type { AdminTreeShelf } from "@/lib/intake-types";

// Camera + WASM are browser-only (docs/11 §5).
const ISBNScanner = dynamic(() => import("./isbn-scanner").then((m) => m.ISBNScanner), {
  ssr: false,
  loading: () => <Skeleton className="aspect-video w-full" />,
});

type Tab = "manual" | "isbn" | "scan";

interface ShelfOption extends AdminTreeShelf {
  group: string;
}

const textareaClass =
  "flex min-h-20 w-full rounded-lg border border-transparent bg-surface-low px-3 py-2 text-sm shadow-sm placeholder:text-outline/70 focus:bg-surface-lowest focus:outline-none focus:ring-2 focus:ring-ring";
const selectClass =
  "flex h-11 w-full rounded-lg border border-transparent bg-surface-low px-3 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring";

export interface AddBookFlowProps {
  /** Closes the hosting dialog (a modal would otherwise make toast actions inert). */
  onClose: () => void;
  /** ISBN handed over from Patron Lookup's "Add this book" — looked up on mount. */
  initialIsbn?: string;
}

export function AddBookFlow({ onClose, initialIsbn }: AddBookFlowProps) {
  const router = useRouter();
  const [tab, setTab] = React.useState<Tab>("isbn");
  const [form, setForm] = React.useState<BookForm>(EMPTY_FORM);
  const [isbnInput, setIsbnInput] = React.useState("");
  const [isbnError, setIsbnError] = React.useState<string | null>(null);
  const [lookupPending, setLookupPending] = React.useState(false);
  const [note, setNote] = React.useState<string | null>(null);
  const [copies, setCopies] = React.useState(1);
  const [shelfPath, setShelfPath] = React.useState("");
  const [shelves, setShelves] = React.useState<ShelfOption[] | null>(null);
  const [errors, setErrors] = React.useState<FieldErrors>({});
  const [submitting, setSubmitting] = React.useState(false);

  // Shelf options for the copy step (GET /api/admin/tree), loaded on first open.
  React.useEffect(() => {
    if (shelves !== null) return;
    let cancelled = false;
    api
      .adminTree()
      .then((tree) => {
        if (cancelled) return;
        const opts: ShelfOption[] = [];
        for (const f of tree.floors)
          for (const s of f.sections)
            for (const a of s.aisles)
              for (const sh of a.shelves)
                opts.push({ ...sh, group: `Floor ${f.floor_number} · ${s.code} ${s.name}` });
        setShelves(opts);
      })
      .catch(() => !cancelled && setShelves([]));
    return () => {
      cancelled = true;
    };
  }, [shelves]);

  const lookup = React.useCallback(
    async (raw: string) => {
      const verdict = classifyBarcode(raw);
      if (verdict.kind !== "isbn") {
        setIsbnError(isbnProblem(verdict.kind));
        return;
      }
      setIsbnError(null);
      setLookupPending(true);
      try {
        const md = await api.fetchIsbn(verdict.isbn);
        setForm((prev) => metadataToForm(md, prev));
        setErrors({});
        setNote(
          md.source === "openlibrary"
            ? "Prefilled from Open Library — review and confirm."
            : "Open Library had no usable record — fill in the details manually.",
        );
        setTab("manual");
      } catch (err) {
        const view = describeIntakeError(err);
        if (view.bookId) {
          const id = view.bookId;
          onClose(); // the modal would otherwise make the toast action inert
          toast.error(view.message, {
            action: { label: "Open in catalog", onClick: () => router.push(`/books/${id}`) },
          });
        } else if (view.suggestManual) {
          setIsbnError(view.message);
          toast.error(view.message, {
            action: {
              label: "Enter manually",
              onClick: () => {
                setForm((f) => ({ ...f, isbn13: verdict.isbn }));
                setTab("manual");
              },
            },
          });
        } else {
          toast.error(view.message);
        }
      } finally {
        setLookupPending(false);
      }
    },
    [router, onClose],
  );

  const handedOver = React.useRef(false);
  React.useEffect(() => {
    if (!initialIsbn || handedOver.current) return;
    handedOver.current = true;
    setIsbnInput(initialIsbn);
    void lookup(initialIsbn);
  }, [initialIsbn, lookup]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const built = buildCreateBody(form, copies, shelfPath);
    if (!built.ok) {
      setErrors(built.errors);
      return;
    }
    setErrors({});
    setSubmitting(true);
    try {
      const book = await api.createAdminBook(built.body);
      // Lets the /admin index-status poller badge the new book while it is embedded.
      window.dispatchEvent(
        new CustomEvent<BookCreatedDetail>(BOOK_CREATED_EVENT, {
          detail: { id: book.id, title: book.title },
        }),
      );
      toast.success(`Added “${book.title}” to the catalog.`, {
        action: { label: "View", onClick: () => router.push(`/books/${book.id}`) },
      });
      onClose();
      router.refresh();
    } catch (err) {
      const view = describeIntakeError(err);
      const id = view.bookId;
      toast.error(
        view.message,
        id
          ? { action: { label: "Open in catalog", onClick: () => router.push(`/books/${id}`) } }
          : undefined,
      );
    } finally {
      setSubmitting(false);
    }
  }

  const set =
    (k: keyof BookForm) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
      setForm((f) => ({ ...f, [k]: e.target.value }));

  const field = (k: keyof BookForm, label: string, opts: { mono?: boolean } = {}) => (
    <div className="space-y-1.5">
      <Label htmlFor={`ab-${k}`}>{label}</Label>
      <Input
        id={`ab-${k}`}
        value={form[k]}
        onChange={set(k)}
        className={opts.mono ? "font-mono" : undefined}
        aria-invalid={Boolean(errors[k])}
      />
      {errors[k] && <p className="text-xs text-destructive">{errors[k]}</p>}
    </div>
  );

  const groups = React.useMemo(() => {
    const m = new Map<string, ShelfOption[]>();
    for (const s of shelves ?? []) m.set(s.group, [...(m.get(s.group) ?? []), s]);
    return [...m];
  }, [shelves]);

  return (
    <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
      <TabsList className="mb-4">
        <TabsTrigger value="manual" className="flex-1">
          <Keyboard className="mr-2 h-4 w-4" aria-hidden />
          Manual
        </TabsTrigger>
        <TabsTrigger value="isbn" className="flex-1">
          <Search className="mr-2 h-4 w-4" aria-hidden />
          ISBN
        </TabsTrigger>
        <TabsTrigger value="scan" className="flex-1">
          <ScanLine className="mr-2 h-4 w-4" aria-hidden />
          Scan ISBN
        </TabsTrigger>
      </TabsList>

      <TabsContent value="isbn">
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            void lookup(isbnInput);
          }}
        >
          <Label htmlFor="ab-isbn-lookup">ISBN-13</Label>
          <div className="flex gap-2">
            <Input
              id="ab-isbn-lookup"
              data-testid="isbn-input"
              inputMode="numeric"
              autoComplete="off"
              placeholder="978…"
              className="font-mono"
              value={isbnInput}
              onChange={(e) => setIsbnInput(e.target.value)}
              aria-invalid={Boolean(isbnError)}
              aria-describedby={isbnError ? "ab-isbn-error" : undefined}
            />
            <Button type="submit" disabled={lookupPending} data-testid="isbn-lookup">
              {lookupPending ? (
                <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
              ) : (
                <Search className="h-4 w-4" aria-hidden />
              )}
              Look up
            </Button>
          </div>
          {isbnError && (
            <p id="ab-isbn-error" className="text-sm text-destructive" data-testid="isbn-error">
              {isbnError}
            </p>
          )}
        </form>
      </TabsContent>

      <TabsContent value="scan">
        {tab === "scan" && (
          <ISBNScanner
            active={!lookupPending}
            onDetected={(isbn) => {
              setIsbnInput(isbn);
              void lookup(isbn);
            }}
            onUseManual={() => setTab("isbn")}
          />
        )}
      </TabsContent>

      <TabsContent value="manual">
        <form className="space-y-4" onSubmit={submit} data-testid="add-book-form">
          {note && (
            <p className="rounded-md bg-surface-container px-3 py-2 text-sm" role="status">
              {note}
            </p>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            {field("isbn13", "ISBN-13", { mono: true })}
            {field("title", "Title")}
            {field("authors", "Authors (comma-separated)")}
            {field("publisher", "Publisher")}
            {field("published_year", "Published year", { mono: true })}
            <div className="space-y-1.5">
              <Label htmlFor="ab-section">Section</Label>
              <select
                id="ab-section"
                className={selectClass}
                value={form.section}
                onChange={set("section")}
              >
                <option value="">Unassigned</option>
                {SECTIONS.map((s) => (
                  <option key={s.code} value={s.code}>
                    {s.code} — {s.name}
                  </option>
                ))}
              </select>
            </div>
            {field("dewey_num", "Dewey", { mono: true })}
            {field("loc_num", "LOC call number", { mono: true })}
            {field("cover_url", "Cover URL")}
            {field("open_library_key", "Open Library key", { mono: true })}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ab-description">Description</Label>
            <textarea
              id="ab-description"
              className={textareaClass}
              value={form.description}
              onChange={set("description")}
            />
          </div>

          <fieldset className="space-y-3 rounded-lg border border-border p-4">
            <legend className="px-1 text-sm font-medium">Copies</legend>
            <p className="text-xs text-on-surface-variant">
              Barcodes (ATH-XXXXXX-CY) are assigned automatically.
            </p>
            <div className="grid gap-3 sm:grid-cols-[8rem_1fr]">
              <div className="space-y-1.5">
                <Label htmlFor="ab-copies">Count</Label>
                <Input
                  id="ab-copies"
                  type="number"
                  min={0}
                  max={20}
                  value={copies}
                  onChange={(e) => setCopies(Number(e.target.value))}
                  aria-invalid={Boolean(errors.copies)}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ab-shelf">Shelf</Label>
                <select
                  id="ab-shelf"
                  className={selectClass}
                  value={shelfPath}
                  onChange={(e) => setShelfPath(e.target.value)}
                  disabled={shelves === null}
                >
                  <option value="">{shelves === null ? "Loading shelves…" : "No shelf yet"}</option>
                  {groups.map(([group, opts]) => (
                    <optgroup key={group} label={group}>
                      {opts.map((s) => (
                        <option key={s.id} value={s.path}>
                          {s.path} ({s.occupied}/{s.total})
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </select>
              </div>
            </div>
            {errors.copies && <p className="text-xs text-destructive">{errors.copies}</p>}
          </fieldset>

          <div className="flex justify-end gap-2">
            <Button type="submit" disabled={submitting} data-testid="add-book-submit">
              {submitting && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
              Add to catalog
            </Button>
          </div>
        </form>
      </TabsContent>
    </Tabs>
  );
}
