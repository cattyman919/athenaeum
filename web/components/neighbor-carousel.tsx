import Link from "next/link";
import { getTranslations } from "next-intl/server";
import { BookCover } from "@/components/book-cover";
import type { Neighbor } from "@/lib/types";

/** "Archival affinity" carousel: other available titles on the same shelf/section. */
export async function NeighborCarousel({ neighbors }: { neighbors: Neighbor[] }) {
  const t = await getTranslations("book.neighbors");
  return (
    <section aria-labelledby="neighbors-title" data-testid="neighbors">
      <h2 id="neighbors-title" className="text-2xl font-semibold tracking-tight">
        {t("title")}
      </h2>
      <p className="mb-4 text-xs text-muted-foreground">{t("subtitle")}</p>
      {neighbors.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("empty")}</p>
      ) : (
        <ul className="flex snap-x gap-4 overflow-x-auto pb-2">
          {neighbors.map((n) => (
            <li key={n.id} className="w-32 shrink-0 snap-start">
              <Link
                href={`/books/${n.id}`}
                prefetch={false}
                data-testid="neighbor-link"
                className="group flex flex-col gap-2"
              >
                <BookCover src={n.cover_url} alt={n.title} title={n.title} />
                <span className="line-clamp-2 text-sm font-medium leading-tight group-hover:text-primary">
                  {n.title}
                </span>
                <span className="line-clamp-1 text-xs text-muted-foreground">
                  {n.authors.join(", ")}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
