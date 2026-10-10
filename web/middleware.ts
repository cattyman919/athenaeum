import { NextResponse, type NextRequest } from "next/server";
import { decideRoute } from "@/lib/route-guard";
import { LOCALE_COOKIE, LOCALE_COOKIE_MAX_AGE, resolveLocale } from "@/lib/locale";

const AT = "athenaeum_at";
const RT = "athenaeum_rt";

function apiBase() {
  return process.env.API_BASE_URL ?? "http://localhost:8080";
}

/** Persists the first resolved locale so the choice stays stable (docs/14 §3). */
function withLocale(req: NextRequest, res: NextResponse): NextResponse {
  if (!req.cookies.get(LOCALE_COOKIE)) {
    const locale = resolveLocale({ acceptLanguage: req.headers.get("accept-language") });
    res.cookies.set(LOCALE_COOKIE, locale, {
      path: "/",
      sameSite: "lax",
      maxAge: LOCALE_COOKIE_MAX_AGE,
      httpOnly: false,
    });
  }
  return res;
}

function redirect(req: NextRequest, path: string): NextResponse {
  return NextResponse.redirect(new URL(path, req.url));
}

function clearSession(res: NextResponse): NextResponse {
  res.cookies.delete(AT);
  res.cookies.delete(RT);
  return res;
}

/** name=value pairs of Set-Cookie headers (attributes dropped). */
function cookiePairs(setCookies: string[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const sc of setCookies) {
    const [pair] = sc.split(";");
    const i = pair.indexOf("=");
    if (i > 0) out[pair.slice(0, i).trim()] = pair.slice(i + 1).trim();
  }
  return out;
}

export async function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  const rt = req.cookies.get(RT)?.value;
  const decision = decideRoute({
    pathname,
    hasAccess: Boolean(req.cookies.get(AT)?.value),
    hasRefresh: Boolean(rt),
  });

  switch (decision.action) {
    case "login": {
      const url = new URL("/login", req.url);
      if (decision.next !== "/") url.searchParams.set("next", decision.next + req.nextUrl.search);
      return withLocale(req, NextResponse.redirect(url));
    }
    case "catalog":
      return withLocale(req, redirect(req, "/catalog"));
    case "refresh": {
      // The 15-minute access cookie lapsed but a refresh token remains: rotate here so
      // server components see a fresh session (docs/04 §5, docs/07 §4).
      let upstream: Response | null = null;
      try {
        upstream = await fetch(`${apiBase()}/api/auth/refresh`, {
          method: "POST",
          headers: { cookie: `${RT}=${rt}` },
          cache: "no-store",
        });
      } catch {
        upstream = null;
      }
      if (!upstream?.ok) {
        if (pathname === "/login") return withLocale(req, clearSession(NextResponse.next()));
        const url = new URL("/login", req.url);
        url.searchParams.set("reason", "expired");
        return withLocale(req, clearSession(NextResponse.redirect(url)));
      }
      const setCookies = upstream.headers.getSetCookie();
      const fresh = cookiePairs(setCookies);
      let res: NextResponse;
      if (pathname === "/login") {
        res = redirect(req, "/catalog");
      } else {
        const headers = new Headers(req.headers);
        const merged = new Map<string, string>();
        for (const c of req.cookies.getAll()) merged.set(c.name, c.value);
        for (const [k, v] of Object.entries(fresh)) merged.set(k, v);
        headers.set("cookie", [...merged].map(([k, v]) => `${k}=${v}`).join("; "));
        res = NextResponse.next({ request: { headers } });
      }
      for (const sc of setCookies) res.headers.append("set-cookie", sc);
      return withLocale(req, res);
    }
    default:
      return withLocale(req, NextResponse.next());
  }
}

export const config = {
  matcher: ["/((?!api/|_next/static|_next/image|favicon.ico|.*\\..*).*)"],
};
