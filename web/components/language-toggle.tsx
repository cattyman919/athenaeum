"use client";

import * as React from "react";
import { useLocale, useTranslations } from "next-intl";
import { LOCALES, LOCALE_COOKIE, LOCALE_COOKIE_MAX_AGE, type Locale } from "@/lib/locale";
import { cn } from "@/lib/utils";

/** Two-state EN ⇄ 中文 toggle (docs/14 §9): writes the locale cookie and reloads. */
export function LanguageToggle({ className }: { className?: string }) {
  const t = useTranslations("language");
  const active = useLocale() as Locale;

  return (
    <div
      role="group"
      aria-label={t("toggleLabel")}
      className={cn(
        "inline-flex items-center rounded-full bg-surface-container p-0.5 text-xs",
        className,
      )}
    >
      {LOCALES.map((loc) => {
        const isActive = loc === active;
        return (
          <button
            key={loc}
            type="button"
            data-testid={`lang-${loc}`}
            aria-pressed={isActive}
            aria-label={t("switchTo", { language: t(loc) })}
            onClick={() => {
              if (isActive) return;
              // The cookie is not httpOnly (docs/14 §3), so write it directly and hard-reload:
              // a server action + soft refresh can lose a race with in-flight link prefetches
              // and leave the page in the old language.
              document.cookie = `${LOCALE_COOKIE}=${loc}; path=/; max-age=${LOCALE_COOKIE_MAX_AGE}; samesite=lax`;
              window.location.reload();
            }}
            className={cn(
              "rounded-full px-2.5 py-1 font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              isActive
                ? "bg-surface-lowest text-primary shadow-sm"
                : "text-on-surface-variant hover:text-on-surface",
            )}
          >
            {t(loc)}
          </button>
        );
      })}
    </div>
  );
}
