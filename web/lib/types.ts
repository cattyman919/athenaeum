// API payload types (docs/03). Hand-written mirrors of the JSON contracts.

export interface SectionRef {
  code: string;
  name: string;
  color: string;
}

export interface Availability {
  total_copies: number;
  available_copies: number;
  state: string;
  soon: string | null;
}

export interface BookSummary {
  id: string;
  title: string;
  authors: string[];
  isbn13: string | null;
  cover_url: string | null;
  section: SectionRef | null;
  location: { path: string; shelf_code: string } | null;
  availability: Availability;
  published_year: number | null;
  score: number;
}

export interface PageMeta {
  page: number;
  per_page: number;
  total: number;
  total_pages: number;
}

export interface SearchResponse {
  items: BookSummary[];
  meta: PageMeta;
}

export interface CopyDetail {
  id: string;
  barcode: string;
  status: string;
  condition: string;
  last_state_change: string;
  location: { path: string; ladder: string } | null;
}

export interface Neighbor {
  id: string;
  title: string;
  authors: string[];
  cover_url: string | null;
}

export interface BookDetail {
  id: string;
  title: string;
  subtitle: string | null;
  authors: string[];
  isbn13: string | null;
  isbn10: string | null;
  publisher: string | null;
  published_year: number | null;
  description: string | null;
  dewey_num: string | null;
  loc_num: string | null;
  cover_url: string | null;
  section: SectionRef | null;
  availability: { total_copies: number; available_copies: number; state: string };
  copies: CopyDetail[];
  wayfinding: { path: string; ladder_steps: string[] } | null;
  neighbors: Neighbor[];
  my_library_entry: { status: string; is_favorite: boolean } | null;
}

export interface LibraryItem {
  kind: "entry";
  book: { id: string; title: string; cover_url: string | null; authors: string[] };
  location: { floor: number; shelf_code: string; path: string } | null;
  status: "want_to_read" | "reading" | "read";
  is_favorite: boolean;
  progress_pct: number | null;
  rating: number | null;
  added_at: string;
}

export interface LibraryResponse {
  items: LibraryItem[];
  meta: PageMeta;
  counts: { total: number; reading: number; read: number; want_to_read: number; favorites: number };
}

export interface WaitlistItem {
  kind: "hold";
  id: string;
  status: "waiting" | "ready";
  position: number;
  ready_at?: string;
  expires_at?: string;
  book: { id: string; title: string; cover_url: string | null; authors: string[] };
}
