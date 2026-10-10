import Link from "next/link";
import { getTranslations } from "next-intl/server";
import { ShieldAlert } from "lucide-react";
import { AddBookDialog } from "@/components/isbn-scanner";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { getCurrentUser } from "@/lib/api-server";

// Screen 6a — Stitch 3177da52… Admin (docs/07 §7, docs/12 screen 6). Admin copy stays English
// (docs/14 §1); only the non-admin FORBIDDEN state is localized. The KPI grid and audit
// stream (GET /api/admin/data, /api/admin/copy-events) extend this page; the Add Book
// dialog with the ISBN scanner tab is wired here (docs/11 §5).
export default async function AdminPage() {
  const user = await getCurrentUser();
  if (user?.role !== "admin") {
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

  return (
    <div className="space-y-6" data-testid="admin-page">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-on-surface-variant">
            Staff Admin
          </p>
          <h1 className="text-2xl font-semibold">Collection management</h1>
        </div>
        <AddBookDialog />
      </header>
    </div>
  );
}
