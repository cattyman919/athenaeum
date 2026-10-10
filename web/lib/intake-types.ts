// Book-intake payloads (docs/03 §5.3 fetch-isbn, §8.2 admin books, §8.4 admin tree).

export type SectionCode = "SF" | "SCI" | "HVA" | "REF" | "PER";

/** POST /api/books/fetch-isbn 200 body. Partial records have source "offline_partial". */
export interface IsbnMetadata {
  isbn13: string;
  title: string;
  authors: string[];
  publisher: string | null;
  published_year: number | null;
  description: string | null;
  cover_url: string | null;
  dewey_num: string | null;
  loc_num: string | null;
  open_library_key: string | null;
  source: "openlibrary" | "offline_partial" | "offline_mock";
  in_catalog: boolean;
  covers?: string[];
  warnings?: string[];
}

export interface AdminCopyInput {
  barcode?: string;
  status?: "available" | "checked_out" | "reserved_waitlist" | "missing_lost";
  condition?: string;
  shelf_path?: string;
}

/** POST /api/admin/books body: book fields + copies (docs/03 §8.2). */
export interface AdminBookCreate {
  isbn13: string;
  title: string;
  authors: string[];
  publisher?: string | null;
  published_year?: number | null;
  description?: string | null;
  cover_url?: string | null;
  dewey_num?: string | null;
  loc_num?: string | null;
  open_library_key?: string | null;
  section?: SectionCode;
  copies: AdminCopyInput[];
}

/** 201 body — the book detail shape; only the id/title are relied on here. */
export interface AdminBookCreated {
  id: string;
  title: string;
  [key: string]: unknown;
}

export interface AdminTreeShelf {
  id: string;
  code: string;
  path: string;
  occupied: number;
  total: number;
}

export interface AdminTree {
  id: string;
  code: string;
  name: string;
  floors: Array<{
    id: string;
    floor_number: number;
    name: string;
    sections: Array<{
      id: string;
      code: string;
      name: string;
      color: string;
      aisles: Array<{ id: string; code: string; shelves: AdminTreeShelf[] }>;
    }>;
  }>;
}
