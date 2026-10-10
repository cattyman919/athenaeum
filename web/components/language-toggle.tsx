"use client";

import * as React from "react";
import { useLocale, useTranslations } from "next-intl";
import { setLocale } from "@/app/actions";
import { LOCALES, type Locale } from "@/lib/locale";
import { cn } from "@/lib/utils";

/** Two-state EN ⇄ 中文 toggle (docs/14 §9): posts to the setLocale server action. */
export function LanguageToggle({ className }: { className?: string }) {
  const t = useTranslations("language");
  const active = useLocale() as Locale;
  const [pending, startTransition] = React.useTransition();

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
            disabled={pending}
            onClick={() => !isActive && startTransition(() => setLocale(loc))}
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
