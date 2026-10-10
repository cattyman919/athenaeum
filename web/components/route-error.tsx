"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { AlertTriangle, RotateCcw } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";

/**
 * Route-level 5xx / unexpected-error state (docs/07 §9): an inline panel with a retry
 * affordance that re-runs the server render (`router.refresh()` + boundary `reset()`).
 */
export function RouteError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const t = useTranslations("common.routeError");
  const router = useRouter();
  const [pending, startTransition] = React.useTransition();

  React.useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <div
      role="alert"
      data-testid="route-error"
      className="mx-auto flex max-w-md flex-col items-center gap-4 py-24 text-center"
    >
      <span className="flex h-12 w-12 items-center justify-center rounded-full bg-status-na-bg text-status-na-fg">
        <AlertTriangle className="h-6 w-6" aria-hidden />
      </span>
      <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
      <p className="text-sm text-muted-foreground">{t("body")}</p>
      {error.digest && (
        <p className="mono-label text-muted-foreground" data-testid="route-error-digest">
          {t("reference", { digest: error.digest })}
        </p>
      )}
      <div className="flex gap-2">
        <Button
          data-testid="route-error-retry"
          disabled={pending}
          onClick={() =>
            startTransition(() => {
              router.refresh();
              reset();
            })
          }
        >
          <RotateCcw className="h-4 w-4" aria-hidden />
          {t("retry")}
        </Button>
        <Link href="/catalog" className={buttonVariants({ variant: "outline" })}>
          {t("toCatalog")}
        </Link>
      </div>
    </div>
  );
}
