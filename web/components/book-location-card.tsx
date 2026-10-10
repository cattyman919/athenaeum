import { getTranslations } from "next-intl/server";
import { Compass } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { BookDetail } from "@/lib/types";
import { ladderSteps } from "@/lib/wayfinding";

/** Turn-by-turn wayfinding ladder to the home shelf (docs/05 §3–5). */
export async function BookLocationCard({ wayfinding }: { wayfinding: BookDetail["wayfinding"] }) {
  const [t, tWay] = await Promise.all([
    getTranslations("book.wayfinding"),
    getTranslations("wayfinding"),
  ]);
  // Localized steps from structured parts (docs/14 §6.1); English prose only as a fallback.
  const steps = wayfinding
    ? wayfinding.parts
      ? ladderSteps(tWay, wayfinding.parts)
      : wayfinding.ladder_steps
    : [];
  return (
    <Card data-testid="book-location-card">
      <CardHeader className="flex-row items-center gap-3 space-y-0">
        <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Compass className="h-5 w-5" aria-hidden />
        </span>
        <div>
          <CardTitle>{t("title")}</CardTitle>
          <p className="mt-1 text-xs text-muted-foreground">{t("subtitle")}</p>
        </div>
      </CardHeader>
      <CardContent>
        {wayfinding ? (
          <>
            <p className="mono-label mb-4 text-muted-foreground" data-testid="wayfinding-path">
              {wayfinding.path}
            </p>
            <ol className="flex flex-col gap-3" data-testid="ladder-steps">
              {steps.map((step, i) => (
                <li key={i} className="flex items-start gap-3">
                  <span className="mono-label flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-secondary text-foreground">
                    {i + 1}
                  </span>
                  <span className="pt-0.5 text-sm font-medium">{step}</span>
                </li>
              ))}
            </ol>
          </>
        ) : (
          <p className="text-sm text-muted-foreground">{t("none")}</p>
        )}
      </CardContent>
    </Card>
  );
}
