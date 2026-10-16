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

/** GET /api/books/search meta: paging plus the auto-index backlog size (docs/03 §5.1). */
export interface SearchMeta extends PageMeta {
  index_pending: number;
}

export interface SearchResponse {
  items: BookSummary[];
  meta: SearchMeta;
}

/** Structured shelf location (docs/03 §5.2); localized via `wayfinding.*` (docs/14 §6.1). */
export interface LocationParts {
  building_code: string;
  building_name: string;
  floor_number: number;
  floor_name: string;
  section_code: string;
  section_name: string;
  aisle_code: string;
  shelf_code: string;
  row_level: number;
  height_tier: string;
}

export interface CopyDetail {
  id: string;
  barcode: string;
  status: string;
  condition: string;
  last_state_change: string;
  location: { path: string; ladder: string; parts: LocationParts } | null;
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
  wayfinding: { path: string; parts: LocationParts; ladder_steps: string[] } | null;
  neighbors: Neighbor[];
  my_library_entry: {
    status: "want_to_read" | "reading" | "read";
    is_favorite: boolean;
    rating: number | null;
  } | null;
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

// AI Librarian chat (docs/03 §7, SSE payloads docs/06 §6).
/** POST /api/chat/transcribe response (docs/15 §4); `text` is "" for silent clips. */
export interface TranscribeResponse {
  text: string;
  language: string;
}

export interface ConversationSummary {
  id: string;
  title: string;
  updated_at: string;
}

export interface ConversationPage {
  items: ConversationSummary[];
  meta: PageMeta;
}

export type ChatRole = "system" | "user" | "assistant" | "tool";

/** messages.tool_meta for tool rows: {tool_call_id, name, args, result} (docs/06 §7). */
export interface ChatToolMeta {
  tool_call_id: string;
  name: string;
  args: unknown;
  result: string;
}

export interface ChatMessage {
  id: string;
  role: ChatRole;
  content: string | null;
  tool_meta: ChatToolMeta | null;
  created_at: string;
  /** Persisted citation cards; assistant messages only (docs/06 §7). */
  citations?: ChatCitation[];
}

export interface ConversationDetail {
  id: string;
  title: string;
  messages: ChatMessage[];
}

export interface ChatCitation {
  book_id: string;
  title: string;
  path: string | null;
  snippet: string;
  score: number;
  /** books.cover_url; null renders the typographic cover placeholder. */
  cover_url?: string | null;
}

export interface ChatMetaEvent {
  conversation_id: string;
  turn: number;
  model: string;
  locale: string;
}

export interface ChatToolCallEvent {
  id: string;
  name: string;
  args: unknown;
}

export interface ChatToolResultEvent {
  id: string;
  summary: string;
}

export interface ChatDoneEvent {
  message_id: string;
  tokens_in: number;
  tokens_out: number;
  finish_reason: "stop" | "rate_limited_daily";
}

export interface ChatErrorEvent {
  code: string;
  message: string;
}

// Patron barcode scanner (docs/03 §5.4, §6.4; docs/11).
export interface ScanLocation {
  path: string;
  parts: LocationParts | null;
  ladder_steps: string[];
}

export interface ScanCopy {
  id: string;
  barcode: string;
  status: string;
  condition: string;
  last_state_change?: string;
  location: ScanLocation | null;
}

export interface ScanResponse {
  code: string;
  code_type: "isbn13" | "copy_barcode";
  in_catalog: boolean;
  match: "book" | "copy" | "none";
  book: {
    id: string;
    title: string;
    authors: string[];
    isbn13: string | null;
    cover_url: string | null;
    section: SectionRef | null;
  } | null;
  copy: ScanCopy | null;
  availability: Pick<Availability, "total_copies" | "available_copies" | "state"> | null;
  wayfinding: ScanLocation | null;
  returnable: boolean;
}

export interface ReturnResponse {
  copy: ScanCopy;
  availability: Pick<Availability, "total_copies" | "available_copies" | "state">;
  hold: { id: string; status: "ready"; expires_at: string } | null;
}
