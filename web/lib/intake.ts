// Add-Book dialog logic (docs/11 §6/§8, docs/03 §5.3/§8.2): metadata → form prefill,
// form → POST /api/admin/books body, and fetch-isbn error → admin-facing message.
// Admin-only surface → English copy (docs/14 §1).

import { ApiError } from "./api-client";
import { classifyBarcode } from "./barcode";
import type { AdminBookCreate, IsbnMetadata, SectionCode } from "./intake-types";

export const SECTIONS: Array<{ code: SectionCode; name: string }> = [
  { code: "SF", name: "Science Fiction & Speculative" },
  { code: "SCI", name: "Science, Mathematics & Tech" },
  { code: "HVA", name: "History, Humanities & Arts" },
  { code: "REF", name: "Reference & Special Folios" },
  { code: "PER", name: "Periodicals & Archives" },
];

export interface BookForm {
  isbn13: string;
  title: string;
  authors: string;
  publisher: string;
  published_year: string;
  description: string;
  cover_url: string;
  dewey_num: string;
  loc_num: string;
  open_library_key: string;
  section: SectionCode | "";
}

export const EMPTY_FORM: BookForm = {
  isbn13: "",
  title: "",
  authors: "",
  publisher: "",
  published_year: "",
  description: "",
  cover_url: "",
  dewey_num: "",
  loc_num: "",
  open_library_key: "",
  section: "",
};

/** Prefills the form from a fetch-isbn record, keeping a chosen section. */
export function metadataToForm(md: IsbnMetadata, prev: BookForm = EMPTY_FORM): BookForm {
  return {
    ...EMPTY_FORM,
    section: prev.section,
    isbn13: md.isbn13,
    title: md.title,
    authors: md.authors.join(", "),
    publisher: md.publisher ?? "",
    published_year: md.published_year ? String(md.published_year) : "",
    description: md.description ?? "",
    cover_url: md.cover_url ?? "",
    dewey_num: md.dewey_num ?? "",
    loc_num: md.loc_num ?? "",
    open_library_key: md.open_library_key ?? "",
  };
}

const orNull = (s: string) => (s.trim() === "" ? null : s.trim());

export type FieldErrors = Partial<Record<keyof BookForm | "copies" | "shelf", string>>;

export type BuildResult = { ok: true; body: AdminBookCreate } | { ok: false; errors: FieldErrors };

/** Validates the form and builds the admin create body (section + shelf required; copies share one shelf). */
export function buildCreateBody(form: BookForm, copies: number, shelfPath: string): BuildResult {
  const errors: FieldErrors = {};
  const verdict = classifyBarcode(form.isbn13);
  if (verdict.kind !== "isbn") errors.isbn13 = isbnProblem(verdict.kind);
  if (!form.title.trim()) errors.title = "Title is required.";
  const authors = form.authors
    .split(",")
    .map((a) => a.trim())
    .filter(Boolean);
  if (authors.length === 0) errors.authors = "At least one author is required.";
  let year: number | null = null;
  if (form.published_year.trim()) {
    year = Number(form.published_year);
    if (!Number.isInteger(year) || year < 0 || year > 9999) {
      errors.published_year = "Year must be a whole number.";
    }
  }
  if (!form.section) errors.section = "Choose a section.";
  if (!shelfPath) errors.shelf = "Choose the shelf these copies live on.";
  if (!Number.isInteger(copies) || copies < 1 || copies > 20) {
    errors.copies = "Copies must be between 1 and 20.";
  }
  if (Object.keys(errors).length > 0 || verdict.kind !== "isbn") return { ok: false, errors };
  const copy = { status: "available" as const, shelf_path: shelfPath };
  return {
    ok: true,
    body: {
      isbn13: verdict.isbn,
      title: form.title.trim(),
      authors,
      publisher: orNull(form.publisher),
      published_year: year,
      description: orNull(form.description),
      cover_url: orNull(form.cover_url),
      dewey_num: orNull(form.dewey_num),
      loc_num: orNull(form.loc_num),
      open_library_key: orNull(form.open_library_key),
      section: form.section as SectionCode,
      copies: Array.from({ length: copies }, () => ({ ...copy })),
    },
  };
}

export function isbnProblem(kind: "upc" | "not_isbn" | "invalid"): string {
  switch (kind) {
    case "upc":
      return "That is a 12-digit store (UPC) code — enter the 13-digit ISBN/EAN-13.";
    case "not_isbn":
      return "That EAN-13 is not an ISBN — ISBNs start with 978 or 979.";
    default:
      return "That ISBN-13 failed checksum validation — check the digits.";
  }
}

export interface IntakeErrorView {
  message: string;
  /** 409 ISBN_CONFLICT → deep link to the existing book. */
  bookId?: string;
  /** 429 → seconds until the bucket resets. */
  retryAfter?: number;
  /** 400 INVALID_ISBN → suggest manual entry. */
  suggestManual?: boolean;
}

/** Maps fetch-isbn / create failures to the docs/11 §8 toasts. */
export function describeIntakeError(err: unknown, nowMs: number = Date.now()): IntakeErrorView {
  if (!(err instanceof ApiError)) {
    return { message: "Network error — check the connection and try again." };
  }
  switch (err.code) {
    case "INVALID_ISBN":
      return { message: "The server rejected that ISBN checksum.", suggestManual: true };
    case "ISBN_CONFLICT": {
      const id = typeof err.body?.book_id === "string" ? err.body.book_id : undefined;
      return { message: "This book is already in the catalog.", bookId: id };
    }
    case "RATE_LIMITED": {
      const secs = retryAfterSeconds(err.headers, nowMs);
      return {
        message:
          secs !== undefined
            ? `Lookup limit reached — try again in ${secs}s.`
            : "Lookup limit reached — try again shortly.",
        retryAfter: secs,
      };
    }
    case "FORBIDDEN":
      return { message: "Only staff accounts can add books." };
    case "NOT_FOUND":
      return { message: err.message || "The selected shelf or section was not found." };
    default:
      return { message: err.message || "Something went wrong — try again." };
  }
}

function retryAfterSeconds(h: Headers | undefined, nowMs: number): number | undefined {
  const ra = Number(h?.get("Retry-After"));
  if (Number.isFinite(ra) && ra > 0) return Math.ceil(ra);
  const reset = Number(h?.get("X-RateLimit-Reset"));
  if (Number.isFinite(reset) && reset > 0) return Math.max(1, Math.ceil(reset - nowMs / 1000));
  return undefined;
}
