import { LoginView } from "@/components/login-view";
import { safeNext } from "@/lib/utils";

// Screen 1 — Stitch b6e00ec9… (no app shell). Docs: 04 §1, 07 §7, 12 screen 1.
export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ next?: string; error?: string; reason?: string }>;
}) {
  const sp = await searchParams;
  return (
    <LoginView
      next={safeNext(sp.next)}
      notice={sp.error === "sso" ? "sso" : sp.reason === "expired" ? "expired" : null}
    />
  );
}
