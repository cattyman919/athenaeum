"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useTranslations } from "next-intl";
import { Library, LogOut, Search } from "lucide-react";
import { useAuth } from "@/providers/auth-provider";
import { LanguageToggle } from "@/components/language-toggle";
import { CommandPalette, OPEN_PALETTE_EVENT } from "@/components/command-palette";
import { ScanNavButton, ScannerHost } from "@/components/barcode-scanner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** Authenticated shell: sticky top nav + footer (docs/12 §6). */
export function AppShell({ children }: { children: React.ReactNode }) {
  const t = useTranslations();
  const pathname = usePathname();
  const { user, isAdmin, signOut } = useAuth();

  const links = [
    { href: "/catalog", label: t("shell.nav.catalog") },
    { href: "/library", label: t("shell.nav.library") },
    { href: "/chat", label: t("shell.nav.chat") },
    ...(isAdmin ? [{ href: "/admin", label: t("shell.nav.admin") }] : []),
  ];

  return (
    <div className="flex min-h-screen flex-col">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-card focus:px-3 focus:py-2"
      >
        {t("shell.skipToContent")}
      </a>
      <header className="sticky top-0 z-40 h-16 border-b border-border bg-background/80 backdrop-blur-md">
        <div className="mx-auto flex h-full max-w-7xl items-center gap-6 px-6">
          <Link href="/catalog" className="flex items-center gap-2 font-semibold tracking-tight">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Library className="h-5 w-5" aria-hidden />
            </span>
            <span>{t("common.appName")}</span>
          </Link>
          <button
            type="button"
            data-testid="open-palette"
            aria-label={t("shell.search")}
            onClick={() => window.dispatchEvent(new Event(OPEN_PALETTE_EVENT))}
            className="hidden h-9 w-64 items-center gap-2 rounded-md border border-border bg-card px-3 text-xs text-muted-foreground md:flex"
          >
            <Search className="h-4 w-4" aria-hidden />
            <span>{t("shell.searchPlaceholder")}</span>
            <kbd className="mono-label ml-auto rounded bg-secondary px-1.5 py-0.5">⌘K</kbd>
          </button>
          <nav aria-label="Primary" className="flex items-center gap-1">
            {links.map((l) => (
              <Link
                key={l.href}
                href={l.href}
                aria-current={pathname.startsWith(l.href) ? "page" : undefined}
                className={cn(
                  "rounded-md px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
                  pathname.startsWith(l.href) && "bg-secondary text-foreground",
                )}
              >
                {l.label}
              </Link>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            {user && <ScanNavButton />}
            <LanguageToggle />
            {user && (
              <div className="flex items-center gap-2">
                <span
                  aria-label={t("shell.account.signedInAs", { name: user.name })}
                  title={user.email}
                  className="flex h-8 w-8 items-center justify-center rounded-full bg-primary text-xs font-semibold text-primary-foreground"
                >
                  {user.name.slice(0, 1).toUpperCase()}
                </span>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => void signOut()}
                  data-testid="sign-out"
                >
                  <LogOut className="h-4 w-4" aria-hidden />
                  <span className="hidden sm:inline">{t("shell.account.signOut")}</span>
                </Button>
              </div>
            )}
          </div>
        </div>
      </header>
      <main id="main" className="mx-auto w-full max-w-7xl flex-1 px-6 py-8">
        {children}
      </main>
      <CommandPalette />
      <ScannerHost />
      <footer className="border-t border-border py-4 text-xs text-muted-foreground">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6">
          <span>
            {t("common.appName")} — {t("shell.footer.tagline")}
          </span>
          <span className="mono-label">{t("shell.footer.version")}</span>
        </div>
      </footer>
    </div>
  );
}
