import Link from "next/link";
import { getTranslations } from "next-intl/server";
import { buttonVariants } from "@/components/ui/button";

// 404 book: full-page "Not in this building's catalog" (docs/07 §9).
export default async function BookNotFound() {
  const t = await getTranslations("book.notFound");
  return (
    <div
      className="mx-auto flex max-w-md flex-col items-center gap-4 py-24 text-center"
      data-testid="book-not-found"
    >
      <h1 className="text-3xl font-bold tracking-tight">{t("title")}</h1>
      <p className="text-sm text-muted-foreground">{t("body")}</p>
      <Link href="/catalog" className={buttonVariants()}>
        {t("back")}
      </Link>
    </div>
  );
}
