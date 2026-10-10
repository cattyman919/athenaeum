import Link from "next/link";
import { getTranslations } from "next-intl/server";
import { ShieldAlert } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

/** Non-admin FORBIDDEN state for /admin* (docs/07 §4) — the only localized admin copy. */
export async function AdminForbidden() {
  const t = await getTranslations("adminGate");
  return (
    <Card className="mx-auto mt-12 max-w-md" data-testid="admin-forbidden">
      <CardContent className="flex flex-col items-center gap-3 p-8 text-center">
        <ShieldAlert className="h-8 w-8 text-destructive" aria-hidden />
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <p className="text-sm text-on-surface-variant">{t("body")}</p>
        <Link href="/catalog" className={buttonVariants({ variant: "outline", size: "sm" })}>
          {t("toCatalog")}
        </Link>
      </CardContent>
    </Card>
  );
}
