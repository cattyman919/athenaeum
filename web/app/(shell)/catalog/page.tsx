import { Suspense } from "react";
import Link from "next/link";
import { getTranslations } from "next-intl/server";
import { BookCard } from "@/components/book-card";
import { CatalogSearchBar, FacetSidebar } from "@/components/catalog-controls";
import { ResultsSkeleton } from "@/components/catalog-skeleton";
import { apiServer } from "@/lib/api-server";
import {
  SECTIONS,
  parseCatalogQuery,
  toApiParams,
  toSearchParams,
  type CatalogQuery,
} from "@/lib/catalog-query";
import type { SearchResponse } from "@/lib/types";
import { sectionLabel } from "@/lib/wayfinding";

const PER_PAGE = 12;

type Result = { ok: true; data: SearchResponse } | { ok: false; code: string };

async function search(query: CatalogQuery): Promise<Result> {
  try {
    const res = await apiServer(`/api/books/search?${toApiParams(query, PER_PAGE)}`);
    if (res.ok) return { ok: true, data: (await res.json()) as SearchResponse };
    const body = (await res.json().catch(() => ({}))) as { error?: string };
    return { ok: false, code: body.error ?? "UNEXPECTED" };
  } catch {
    return { ok: false, code: "UNEXPECTED" };
  }
}

function href(query: CatalogQuery, patch: Partial<CatalogQuery>) {
  const qs = toSearchParams({ ...query, ...patch }).toString();
  return qs ? `/catalog?${qs}` : "/catalog";
}

async function Results({ query }: { query: CatalogQuery }) {
  const [t, tWay, result] = await Promise.all([
    getTranslations("catalog"),
    getTranslations("wayfinding"),
    search(query),
  ]);
  return (
    <>
      {!result.ok ? (
        <div
          role="alert"
          data-testid="catalog-error"
          className="flex flex-col items-start gap-3 rounded-lg border border-border bg-card p-6 shadow-sm"
        >
          <h2 className="text-lg font-semibold">
            {result.code === "EMBEDDING_UNAVAILABLE"
              ? t("error.embedding")
              : result.code === "RATE_LIMITED"
                ? t("error.rateLimited")
                : t("error.generic")}
          </h2>
          {result.code === "EMBEDDING_UNAVAILABLE" && (
            <p className="text-sm text-muted-foreground">{t("error.embeddingHint")}</p>
          )}
          <div className="flex gap-2">
            {result.code === "EMBEDDING_UNAVAILABLE" && (
              <Link
                prefetch={false}
                href={href(query, { mode: "keyword", page: 1 })}
                className="inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
              >
                {t("error.switchKeyword")}
              </Link>
            )}
            <Link
              prefetch={false}
              href={href(query, {})}
              className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-medium hover:bg-secondary"
            >
              {t("error.retry")}
            </Link>
          </div>
        </div>
      ) : result.data.items.length === 0 ? (
        <div
          data-testid="catalog-empty"
          className="flex flex-col items-center gap-4 rounded-lg border border-dashed border-border bg-card px-6 py-12 text-center"
        >
          <span className="rounded-full bg-secondary px-4 py-1.5 text-sm font-medium">
            {t("empty.title")}
          </span>
          <p className="max-w-md text-sm text-muted-foreground">{t("empty.hint")}</p>
          <div className="flex flex-wrap justify-center gap-2" aria-label={t("empty.nearby")}>
            {SECTIONS.map((s) => (
              <Link
                prefetch={false}
                key={s.code}
                href={href(
                  { ...query, q: "", sections: [], eras: [], availableOnly: false },
                  { sections: [s.code] },
                )}
                className="inline-flex items-center gap-2 rounded-full border border-border px-3 py-1 text-xs font-medium hover:bg-secondary"
              >
                <span
                  className="h-2 w-2 rounded-full"
                  style={{ backgroundColor: s.color }}
                  aria-hidden
                />
                {sectionLabel(tWay, s.code, s.name)}
              </Link>
            ))}
          </div>
        </div>
      ) : (
        <>
          <p className="text-xs font-medium text-muted-foreground" data-testid="result-count">
            {t("results.count", { count: result.data.meta.total })}
          </p>
          <ul
            className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-4"
            data-testid="book-grid"
          >
            {result.data.items.map((b) => (
              <li key={b.id}>
                <BookCard book={b} />
              </li>
            ))}
          </ul>
          {result.data.meta.total_pages > 1 && (
            <nav
              aria-label={t("results.pagination")}
              className="flex items-center justify-between border-t border-border pt-4 text-sm"
            >
              {query.page > 1 ? (
                <Link
                  prefetch={false}
                  href={href(query, { page: query.page - 1 })}
                  rel="prev"
                  className="font-medium text-primary hover:underline"
                >
                  {t("results.prev")}
                </Link>
              ) : (
                <span />
              )}
              <span className="text-muted-foreground">
                {t("results.page", {
                  page: result.data.meta.page,
                  pages: result.data.meta.total_pages,
                })}
              </span>
              {query.page < result.data.meta.total_pages ? (
                <Link
                  prefetch={false}
                  href={href(query, { page: query.page + 1 })}
                  rel="next"
                  className="font-medium text-primary hover:underline"
                >
                  {t("results.next")}
                </Link>
              ) : (
                <span />
              )}
            </nav>
          )}
        </>
      )}
    </>
  );
}

// Screen 2 — Stitch e9f24637… Catalog Browser (docs/07 §7, docs/12 screen 2).
export default async function CatalogPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const query = parseCatalogQuery(await searchParams);
  const t = await getTranslations("catalog");

  return (
    <div className="grid gap-8 lg:grid-cols-[16rem_1fr]">
      <FacetSidebar query={query} />

      <section className="flex min-w-0 flex-col gap-6" aria-labelledby="catalog-title">
        <header className="flex flex-col gap-1">
          <h1 id="catalog-title" className="text-3xl font-bold tracking-tight">
            {t("title")}
          </h1>
          <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
        </header>

        <CatalogSearchBar query={query} />

        <Suspense key={JSON.stringify(query)} fallback={<ResultsSkeleton />}>
          <Results query={query} />
        </Suspense>
      </section>
    </div>
  );
}
