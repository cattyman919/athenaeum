"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";

/** Re-runs the server render of the current route (docs/07 §9 retry affordance). */
export function RetryButton({ testId = "retry" }: { testId?: string }) {
  const t = useTranslations("common");
  const router = useRouter();
  const [pending, startTransition] = React.useTransition();
  return (
    <Button
      variant="outline"
      size="sm"
      data-testid={testId}
      disabled={pending}
      onClick={() => startTransition(() => router.refresh())}
    >
      <RotateCcw className="h-4 w-4" aria-hidden />
      {t("retry")}
    </Button>
  );
}
