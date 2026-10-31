// Browser API client (docs/07 §4–5): same-origin /api/* proxy, single 401 → refresh retry,
// typed functions mirroring docs/03 route-for-route.

import type { Locale } from "./locale";
import type { AdminBookCreate, AdminBookCreated, AdminTree, IsbnMetadata } from "./intake-types";
import type {
  AdminBookPage,
  AdminCopy,
  AdminKpis,
  AdminUserPage,
  CopyCreate,
  CopyEvent,
  CopyPatch,
  IndexStatus,
  ShelfInspectorData,
  UserRole,
} from "./admin-types";
import type {
  BorrowBookResponse,
  BorrowResponse,
  ConversationDetail,
  ConversationPage,
  ConversationSummary,
  IdentifyResponse,
  IsbnAttachResponse,
  LoansResponse,
  MergeBooksResponse,
  RenewResponse,
  ReturnBookResponse,
  ReturnResponse,
  ScanResponse,
  SearchResponse,
  TranscribeResponse,
} from "./types";

export interface User {
  id: string;
  email: string;
  name: string;
  role: "user" | "admin";
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public body?: Record<string, unknown>,
    public headers?: Headers,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type Fetcher = typeof fetch;

export interface ClientOptions {
  fetcher?: Fetcher;
  /** Called when the session cannot be refreshed (second 401). */
  onExpired?: () => void;
}

/** Parses a docs/03 JSON error envelope into an ApiError. */
export async function toError(res: Response): Promise<ApiError> {
  let body: Record<string, unknown> | undefined;
  try {
    body = (await res.json()) as Record<string, unknown>;
  } catch {
    body = undefined;
  }
  const code = typeof body?.error === "string" ? body.error : "UNEXPECTED";
  const message = typeof body?.message === "string" ? body.message : res.statusText;
  return new ApiError(res.status, code, message, body, res.headers);
}

/** Creates a client; exported as a factory so the retry logic is unit-testable. */
export function createApiClient(opts: ClientOptions = {}) {
  const doFetch: Fetcher = opts.fetcher ?? ((...a) => fetch(...a));
  let refreshing: Promise<boolean> | null = null;

  const refresh = (): Promise<boolean> => {
    // De-duplicate concurrent refreshes so one rotation serves every waiting request.
    refreshing ??= doFetch("/api/auth/refresh", { method: "POST", credentials: "same-origin" })
      .then((r) => r.ok)
      .catch(() => false)
      .finally(() => {
        refreshing = null;
      });
    return refreshing;
  };

  async function request(path: string, init: RequestInit = {}): Promise<Response> {
    const withDefaults: RequestInit = { credentials: "same-origin", ...init };
    let res = await doFetch(path, withDefaults);
    const isAuthRoute = path.startsWith("/api/auth/");
    if (res.status === 401 && !isAuthRoute) {
      if (await refresh()) {
        res = await doFetch(path, withDefaults);
      }
      if (res.status === 401) {
        opts.onExpired?.();
      }
    }
    return res;
  }

  async function json<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    // FormData bodies keep the browser-generated multipart boundary.
    if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    const res = await request(path, { ...init, headers });
    if (!res.ok) throw await toError(res);
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  return {
    request,
    json,
    me: () => json<{ user: User }>("/api/auth/me").then((r) => r.user),
    login: (email: string, password: string) =>
      json<{ user: User }>("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      }).then((r) => r.user),
    register: (name: string, email: string, password: string) =>
      json<{ user: User }>("/api/auth/register", {
        method: "POST",
        body: JSON.stringify({ name, email, password }),
      }).then((r) => r.user),
    logout: () => json<void>("/api/auth/logout", { method: "POST" }),

    // Catalog search (docs/03 §5.1); `params` is the catalog query string (lib/catalog-query).
    searchBooks: (params: URLSearchParams | string) =>
      json<SearchResponse>(`/api/books/search?${params}`),

    // Personal library (docs/03 §6.2). `force` bypasses the soft 409 BOOK_NOT_AVAILABLE.
    addToLibrary: (bookId: string, status?: ReadingStatus, opts: { force?: boolean } = {}) =>
      json<LibraryEntry>(`/api/library${opts.force ? "?force=true" : ""}`, {
        method: "POST",
        body: JSON.stringify(status ? { book_id: bookId, status } : { book_id: bookId }),
      }),
    patchLibrary: (bookId: string, patch: LibraryPatch) =>
      json<LibraryEntry>(`/api/library/${encodeURIComponent(bookId)}`, {
        method: "PATCH",
        body: JSON.stringify(patch),
      }),
    removeFromLibrary: (bookId: string) =>
      json<void>(`/api/library/${encodeURIComponent(bookId)}`, { method: "DELETE" }),

    // Waitlist (docs/03 §6.3).
    joinWaitlist: (bookId: string) =>
      json<WaitlistJoin>(`/api/books/${encodeURIComponent(bookId)}/waitlist`, { method: "POST" }),
    leaveWaitlist: (bookId: string) =>
      json<void>(`/api/books/${encodeURIComponent(bookId)}/waitlist`, { method: "DELETE" }),

    // Patron scanner (docs/03 §5.4, §6.4; docs/11): local ISBN / copy-tag resolve and
    // self-service return of a checked_out copy.
    scanCode: (code: string) =>
      json<ScanResponse>("/api/books/scan", { method: "POST", body: JSON.stringify({ code }) }),
    identifyIsbn: (isbn: string) =>
      json<IdentifyResponse>("/api/books/identify-isbn", {
        method: "POST",
        body: JSON.stringify({ isbn }),
      }),
    borrowCopy: (copyId: string) =>
      json<BorrowResponse>(`/api/copies/${encodeURIComponent(copyId)}/borrow`, {
        method: "POST",
      }),
    renewCopy: (copyId: string) =>
      json<RenewResponse>(`/api/copies/${encodeURIComponent(copyId)}/renew`, {
        method: "POST",
      }),
    listLoans: () => json<LoansResponse>("/api/loans"),
    returnCopy: (copyId: string) =>
      json<ReturnResponse>(`/api/copies/${encodeURIComponent(copyId)}/return`, {
        method: "POST",
      }),

    // Book-level borrow/return (docs/03 §6.5): server picks the copy for the patron.
    borrowBook: (bookId: string) =>
      json<BorrowBookResponse>(`/api/books/${encodeURIComponent(bookId)}/borrow`, {
        method: "POST",
      }),
    returnBook: (bookId: string) =>
      json<ReturnBookResponse>(`/api/books/${encodeURIComponent(bookId)}/return`, {
        method: "POST",
      }),

    // Book intake (docs/03 §5.3, §8.2, §8.4; docs/11). Admin only.
    fetchIsbn: (isbn: string) =>
      json<IsbnMetadata>("/api/books/fetch-isbn", {
        method: "POST",
        body: JSON.stringify({ isbn }),
      }),
    createAdminBook: (body: AdminBookCreate) =>
      json<AdminBookCreated>("/api/admin/books", { method: "POST", body: JSON.stringify(body) }),
    adminTree: () => json<AdminTree>("/api/admin/tree"),

    // Admin ISBN editions (docs/03 §8.6).
    attachIsbn: (bookId: string, isbn13: string) =>
      json<IsbnAttachResponse>(`/api/admin/books/${encodeURIComponent(bookId)}/isbns`, {
        method: "POST",
        body: JSON.stringify({ isbn13 }),
      }),
    detachIsbn: (bookId: string, isbn13: string) =>
      json<void>(
        `/api/admin/books/${encodeURIComponent(bookId)}/isbns/${encodeURIComponent(isbn13)}`,
        { method: "DELETE" },
      ),
    deleteAdminBook: (id: string) =>
      json<void>(`/api/admin/books/${encodeURIComponent(id)}`, { method: "DELETE" }),
    bulkDeleteAdminBooks: (ids: string[]) =>
      json<void>("/api/admin/books/bulk-delete", {
        method: "POST",
        body: JSON.stringify({ ids }),
      }),
    mergeBooks: (sourceId: string, intoId: string) =>
      json<MergeBooksResponse>(`/api/admin/books/${encodeURIComponent(sourceId)}/merge`, {
        method: "POST",
        body: JSON.stringify({ into: intoId }),
      }),

    // Admin portal (docs/03 §8.1, §8.3–8.5).
    adminData: () => json<AdminKpis>("/api/admin/data"),
    adminCopyEvents: (since: string | null, limit = 50) => {
      const q = new URLSearchParams({ limit: String(limit) });
      if (since) q.set("since", since);
      return json<{ items: CopyEvent[] }>(`/api/admin/copy-events?${q}`).then((r) => r.items);
    },
    /** Auto-index worker backlog: books still waiting for embeddings. */
    adminIndexStatus: () => json<IndexStatus>("/api/admin/index-status"),
    adminShelf: (id: string) =>
      json<ShelfInspectorData>(`/api/admin/shelves/${encodeURIComponent(id)}`),
    /** `ref` is a copy id or barcode. */
    patchAdminCopy: (ref: string, patch: CopyPatch) =>
      json<AdminCopy>(`/api/admin/copies/${encodeURIComponent(ref)}`, {
        method: "PATCH",
        body: JSON.stringify(patch),
      }),
    createAdminCopy: (body: CopyCreate) =>
      json<AdminCopy>("/api/admin/copies", { method: "POST", body: JSON.stringify(body) }),
    deleteAdminCopy: (ref: string) =>
      json<void>(`/api/admin/copies/${encodeURIComponent(ref)}`, { method: "DELETE" }),
    adminBooks: (
      params: {
        search?: string;
        shelf?: string;
        page?: number;
        per_page?: number;
        sort?: string;
      } = {},
    ) => {
      const q = new URLSearchParams();
      for (const [k, v] of Object.entries(params))
        if (v !== undefined && v !== "") q.set(k, String(v));
      return json<AdminBookPage>(`/api/admin/books${q.size ? `?${q}` : ""}`);
    },
    adminUsers: (page = 1, perPage = 50) =>
      json<AdminUserPage>(`/api/admin/users?page=${page}&per_page=${perPage}`),
    patchAdminUser: (id: string, role: UserRole) =>
      json<User>(`/api/admin/users/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify({ role }),
      }),

    // AI Librarian conversations (docs/03 §7). The SSE turn lives in hooks/use-chat-stream.ts.
    listConversations: (perPage = 50) =>
      json<ConversationPage>(`/api/chat/conversations?per_page=${perPage}`),
    createConversation: (title?: string) =>
      json<ConversationSummary>("/api/chat/conversations", {
        method: "POST",
        body: JSON.stringify(title ? { title } : {}),
      }),
    getConversation: (id: string) =>
      json<ConversationDetail>(`/api/chat/conversations/${encodeURIComponent(id)}`),
    deleteConversation: (id: string) =>
      json<void>(`/api/chat/conversations/${encodeURIComponent(id)}`, { method: "DELETE" }),

    // Push-to-talk transcription (docs/03 §7.2, docs/15 §4): multipart `audio` + optional
    // `locale` hint. `text` may be "" for silent clips; the caller inserts it into the composer.
    /** `partial: true` → live interim pass over the cumulative clip (docs/15 §2a). */
    transcribe: (
      audio: Blob,
      locale?: Locale,
      fileName = "clip.webm",
      opts: { partial?: boolean } = {},
    ) => {
      const form = new FormData();
      form.append("audio", audio, fileName);
      if (locale) form.append("locale", locale);
      const path = opts.partial ? "/api/chat/transcribe?partial=true" : "/api/chat/transcribe";
      return json<TranscribeResponse>(path, { method: "POST", body: form });
    },
  };
}

export type ReadingStatus = "want_to_read" | "reading" | "read";

export interface LibraryPatch {
  status?: ReadingStatus;
  is_favorite?: boolean;
  progress_pct?: number | null;
  rating?: number | null;
}

export interface LibraryEntry {
  status: ReadingStatus;
  is_favorite: boolean;
  progress_pct: number | null;
  rating: number | null;
}

export interface WaitlistJoin {
  id: string;
  status: "waiting" | "ready";
  position: number;
}

/** True for the soft 409 raised when a zero-copy book is first marked `reading`. */
export function isBookNotAvailable(err: unknown): err is ApiError {
  return err instanceof ApiError && err.status === 409 && err.code === "BOOK_NOT_AVAILABLE";
}

function expireRedirect() {
  if (typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
    window.location.assign("/login?reason=expired");
  }
}

/** Default browser client. */
export const api = createApiClient({ onExpired: expireRedirect });
