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
