// Pure routing decision for middleware.ts (docs/07 §2): kept side-effect free for unit tests.

export type GuardDecision =
  | { action: "next" }
  | { action: "refresh" } // access token missing but refresh token present
  | { action: "login"; next: string }
  | { action: "catalog" };

const PUBLIC_PREFIXES = ["/login"];

export function isPublicPath(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

export function decideRoute(input: {
  pathname: string;
  hasAccess: boolean;
  hasRefresh: boolean;
}): GuardDecision {
  const { pathname, hasAccess, hasRefresh } = input;
  const hasSession = hasAccess || hasRefresh;
  if (isPublicPath(pathname)) {
    // Authenticated users requesting /login go to the catalog.
    return hasAccess
      ? { action: "catalog" }
      : hasRefresh
        ? { action: "refresh" }
        : { action: "next" };
  }
  if (!hasSession) return { action: "login", next: pathname };
  if (!hasAccess) return { action: "refresh" };
  return { action: "next" };
}
