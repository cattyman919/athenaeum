import { getFormatter, getTranslations } from "next-intl/server";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { enumLabel } from "@/lib/enum-labels";
import type { CopyDetail } from "@/lib/types";

const VARIANT: Record<string, "available" | "out" | "unavailable"> = {
  available: "available",
  checked_out: "out",
  reserved_waitlist: "out",
  missing_lost: "unavailable",
};

/** Physical copy manifest: one row per `book_copies` row (docs/05 §5). */
export async function CopyManifest({ copies }: { copies: CopyDetail[] }) {
  const [t, tEnums, format] = await Promise.all([
    getTranslations("book.manifest"),
    getTranslations("enums"),
    getFormatter(),
  ]);
  return (
    <Card data-testid="copy-manifest">
      <CardHeader className="flex-row items-baseline justify-between space-y-0">
        <CardTitle>{t("title")}</CardTitle>
        <span className="text-xs text-muted-foreground">
          {t("count", { count: copies.length })}
        </span>
      </CardHeader>
      <CardContent>
        {copies.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("empty")}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-border text-xs uppercase tracking-wider text-muted-foreground">
                  <th className="py-2 pr-4 font-medium">{t("barcode")}</th>
                  <th className="py-2 pr-4 font-medium">{t("status")}</th>
                  <th className="py-2 pr-4 font-medium">{t("location")}</th>
                  <th className="py-2 font-medium">{t("changed")}</th>
                </tr>
              </thead>
              <tbody>
                {copies.map((c) => (
                  <tr
                    key={c.id}
                    className="border-b border-border/60 last:border-0"
                    data-testid="copy-row"
                  >
                    <td className="mono-label py-3 pr-4">{c.barcode}</td>
                    <td className="py-3 pr-4">
                      <Badge
                        variant={VARIANT[c.status] ?? "default"}
                        data-testid="copy-status"
                        data-state={c.status}
                      >
                        {enumLabel(tEnums, "copyStatus", c.status)}
                      </Badge>
                    </td>
                    <td className="mono-label py-3 pr-4 text-muted-foreground">
                      {c.location?.path ?? t("unshelved")}
                    </td>
                    <td className="py-3 text-xs text-muted-foreground">
                      {format.dateTime(new Date(c.last_state_change), { dateStyle: "medium" })}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
