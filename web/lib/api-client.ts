// Browser API client (docs/07 §4–5): same-origin /api/* proxy, single 401 → refresh retry,
// typed functions mirroring docs/03 route-for-route.

import type { ConversationDetail, ConversationPage, ConversationSummary } from "./types";

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
    if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
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
