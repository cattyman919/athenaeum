import "server-only";
import { cookies } from "next/headers";
import type { User } from "./api-client";

const API_BASE = process.env.API_BASE_URL ?? "http://localhost:8080";

/** Server-component fetch: forwards the session cookies to the API (docs/07 §4). */
export async function apiServer(path: string, init: RequestInit = {}): Promise<Response> {
  const store = await cookies();
  const cookie = ["athenaeum_at", "athenaeum_rt"]
    .map((n) => {
      const v = store.get(n)?.value;
      return v ? `${n}=${v}` : null;
    })
    .filter(Boolean)
    .join("; ");
  const headers = new Headers(init.headers);
  if (cookie) headers.set("Cookie", cookie);
  return fetch(`${API_BASE}${path}`, { ...init, headers, cache: "no-store" });
}

/** Current user for server components, or null when unauthenticated. */
export async function getCurrentUser(): Promise<User | null> {
  try {
    const res = await apiServer("/api/auth/me");
    if (!res.ok) return null;
    return ((await res.json()) as { user: User }).user;
  } catch {
    return null;
  }
}
