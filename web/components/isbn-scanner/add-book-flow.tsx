"use client";

// Admin Add-Book flow (docs/11 §2c/§6, docs/07 screen 6a): Manual / ISBN / Scan tabs.
// ISBN + Scan call POST /api/books/fetch-isbn and prefill the Manual form; the cataloger
// confirms and the copy step posts POST /api/admin/books. Hosted by the /admin AddBookDialog
// and the scanner's Staff Intake tab. Admin-only → English (docs/14 §1).

import * as React from "react";
import dynamic from "next/dynamic";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import {
  BookCopy,
  ChevronDown,
  Keyboard,
  Loader2,
  MapPin,
  ScanLine,
  Search,
  X,
} from "lucide-react";
import { BookCover } from "@/components/book-cover";
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
import type { AdminTreeShelf, SimilarBook } from "@/lib/intake-types";

// Camera + WASM are browser-only (docs/11 §5).
const ISBNScanner = dynamic(() => import("./isbn-scanner").then((m) => m.ISBNScanner), {
  ssr: false,
  loading: () => <Skeleton className="aspect-video w-full" />,
});

type Tab = "manual" | "isbn" | "scan";

interface ShelfOption extends AdminTreeShelf {
  group: string;
  sectionCode: string;
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
  const [similar, setSimilar] = React.useState<SimilarBook[] | null>(null);
  const [similarDismissed, setSimilarDismissed] = React.useState(false);
  const [attachingEdition, setAttachingEdition] = React.useState(false);
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
                opts.push({
                  ...sh,
                  sectionCode: s.code,
                  group: `Floor ${f.floor_number} · ${s.code} ${s.name}`,
                });
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
        setSimilar(md.similar && md.similar.length > 0 ? md.similar : null);
        setSimilarDismissed(false);
        // Pre-fill section from the first similar book if available.
        if (md.similar && md.similar.length > 0 && md.similar[0].section) {
          const sec = md.similar[0].section.code;
          setForm((prev) => ({ ...prev, section: sec }));
          setShelfPath("");
        }
        setNote(
          md.source === "openlibrary"
            ? null
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

  async function attachEdition() {
    if (!similar || similar.length === 0) return;
    const target = similar[0];
    if (!shelfPath) {
      setErrors((e) => ({ ...e, shelf: "Choose the shelf these copies live on." }));
      return;
    }
    if (!form.section) {
      setErrors((e) => ({ ...e, section: "Choose a section." }));
      return;
    }
    setAttachingEdition(true);
    try {
      await api.attachIsbn(target.id, form.isbn13);
      // Create the requested number of copies on the chosen shelf.
      await Promise.all(
        Array.from({ length: copies }, () =>
          api.createAdminCopy({ book_id: target.id, status: "available", shelf_path: shelfPath }),
        ),
      );
      toast.success(
        `Added ${copies} ${copies === 1 ? "copy" : "copies"} of "${target.title}" — edition attached.`,
        {
          action: { label: "View", onClick: () => router.push(`/books/${target.id}`) },
        },
      );
      onClose();
      router.refresh();
    } catch (err) {
      const view = describeIntakeError(err);
      toast.error(view.message);
    } finally {
      setAttachingEdition(false);
    }
  }

  const set =
    (k: keyof BookForm) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
      setForm((f) => ({ ...f, [k]: e.target.value }));

  const field = (
    k: keyof BookForm,
    label: string,
    opts: { mono?: boolean; required?: boolean } = {},
  ) => (
    <div className="space-y-1.5">
      <Label htmlFor={`ab-${k}`}>
        {label}
        {opts.required && <span className="text-destructive"> *</span>}
      </Label>
      <Input
        id={`ab-${k}`}
        value={form[k]}
        onChange={set(k)}
        className={opts.mono ? "font-mono" : undefined}
        aria-invalid={Boolean(errors[k])}
        aria-required={opts.required || undefined}
      />
      {errors[k] && <p className="text-xs text-destructive">{errors[k]}</p>}
    </div>
  );

  const groups = React.useMemo(() => {
    const m = new Map<string, ShelfOption[]>();
    for (const s of shelves ?? []) {
      if (form.section && s.sectionCode !== form.section) continue;
      m.set(s.group, [...(m.get(s.group) ?? []), s]);
    }
    return [...m];
  }, [shelves, form.section]);

  const coverSrc = form.cover_url.trim() || null;

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
        <form className="space-y-5" onSubmit={submit} data-testid="add-book-form" noValidate>
          {similar && !similarDismissed && (
            <div
              className="space-y-3 rounded-lg border border-border bg-surface-container p-4"
              data-testid="intake-similar"
            >
              <div className="flex items-start gap-3">
                <BookCopy className="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden />
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium">
                    Looks like <em>{similar[0].title}</em> is already in the catalog
                  </p>
                  <p className="mt-0.5 text-xs text-on-surface-variant">
                    {similar[0].authors.join(", ")} · {similar[0].copies}{" "}
                    {similar[0].copies === 1 ? "copy" : "copies"} held
                  </p>
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  size="sm"
                  onClick={() => void attachEdition()}
                  disabled={attachingEdition}
                  data-testid="intake-attach"
                >
                  {attachingEdition && <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />}
                  Add as another edition of this book
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => setSimilarDismissed(true)}
                  data-testid="intake-create-new"
                >
                  <X className="h-3.5 w-3.5" aria-hidden />
                  Create a separate book anyway
                </Button>
              </div>
            </div>
          )}

          {note && (
            <p className="rounded-md bg-surface-container px-3 py-2 text-sm" role="status">
              {note}
            </p>
          )}

          <div className="grid gap-5 md:grid-cols-[10rem_1fr]">
            <div className="space-y-2">
              <div className="mx-auto w-32 shadow-md md:w-full" data-testid="intake-cover-preview">
                <BookCover
                  key={coverSrc ?? "none"}
                  src={coverSrc}
                  alt={form.title ? `Cover of ${form.title}` : "Cover preview"}
                  title={form.title || "No cover yet"}
                />
              </div>
              <p className="text-center text-[11px] text-on-surface-variant">
                {coverSrc ? "Cover preview" : "No cover — add a URL under Catalog numbers"}
              </p>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              {field("isbn13", "ISBN-13", { mono: true })}
              {field("title", "Title", { required: true })}
              <div className="sm:col-span-2">
                {field("authors", "Authors (comma-separated)", { required: true })}
              </div>
              {field("publisher", "Publisher")}
              {field("published_year", "Published year", { mono: true })}
            </div>
          </div>

          <fieldset className="space-y-3 rounded-lg border border-border p-4">
            <legend className="flex items-center gap-1.5 px-1 text-sm font-medium">
              <MapPin className="h-4 w-4 text-primary" aria-hidden />
              Shelving
            </legend>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="ab-section">
                  Section <span className="text-destructive">*</span>
                </Label>
                <select
                  id="ab-section"
                  className={selectClass}
                  value={form.section}
                  onChange={(e) => {
                    set("section")(e);
                    setShelfPath("");
                  }}
                  aria-invalid={Boolean(errors.section)}
                  aria-required
                >
                  <option value="">Choose a section…</option>
                  {SECTIONS.map((s) => (
                    <option key={s.code} value={s.code}>
                      {s.code} — {s.name}
                    </option>
                  ))}
                </select>
                {errors.section && (
                  <p className="text-xs text-destructive" data-testid="err-section">
                    {errors.section}
                  </p>
                )}
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ab-shelf">
                  Shelf <span className="text-destructive">*</span>
                </Label>
                <select
                  id="ab-shelf"
                  className={selectClass}
                  value={shelfPath}
                  onChange={(e) => setShelfPath(e.target.value)}
                  disabled={shelves === null || !form.section}
                  aria-invalid={Boolean(errors.shelf)}
                  aria-required
                >
                  <option value="">
                    {shelves === null
                      ? "Loading shelves…"
                      : form.section
                        ? "Choose a shelf…"
                        : "Choose a section first"}
                  </option>
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
                {errors.shelf && (
                  <p className="text-xs text-destructive" data-testid="err-shelf">
                    {errors.shelf}
                  </p>
                )}
              </div>
            </div>
            <div className="grid gap-3 sm:grid-cols-[8rem_1fr] sm:items-end">
              <div className="space-y-1.5">
                <Label htmlFor="ab-copies">Copies</Label>
                <Input
                  id="ab-copies"
                  type="number"
                  min={1}
                  max={20}
                  value={copies}
                  onChange={(e) => setCopies(Number(e.target.value))}
                  aria-invalid={Boolean(errors.copies)}
                />
              </div>
              <p className="text-xs text-on-surface-variant">
                Barcodes (ATH-XXXXXX-CY) are assigned automatically.
              </p>
            </div>
            {errors.copies && <p className="text-xs text-destructive">{errors.copies}</p>}
          </fieldset>

          <details className="group rounded-lg border border-border">
            <summary className="flex cursor-pointer list-none items-center justify-between px-4 py-3 text-sm font-medium">
              Catalog numbers &amp; description
              <ChevronDown
                className="h-4 w-4 transition-transform group-open:rotate-180"
                aria-hidden
              />
            </summary>
            <div className="space-y-3 border-t border-border p-4">
              <div className="grid gap-3 sm:grid-cols-2">
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
            </div>
          </details>

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
