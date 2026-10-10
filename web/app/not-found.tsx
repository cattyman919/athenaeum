import Link from "next/link";
import { getTranslations } from "next-intl/server";
import { Compass } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";

// Unknown URL (docs/07 §9). Book ids have their own "Not in this building's catalog" page.
export default async function NotFound() {
  const t = await getTranslations("common.notFound");
  return (
    <main
      className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center gap-4 px-6 text-center"
      data-testid="route-not-found"
    >
      <span className="flex h-12 w-12 items-center justify-center rounded-full bg-secondary text-primary">
        <Compass className="h-6 w-6" aria-hidden />
      </span>
      <h1 className="text-3xl font-bold tracking-tight">{t("title")}</h1>
      <p className="text-sm text-muted-foreground">{t("body")}</p>
      <Link href="/catalog" className={buttonVariants()}>
        {t("back")}
      </Link>
    </main>
  );
}
