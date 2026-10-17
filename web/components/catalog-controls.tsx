"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { ChevronDown, ScanLine, Search, X } from "lucide-react";
import { openScanner } from "@/components/barcode-scanner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  DEFAULT_QUERY,
  ERAS,
  MODES,
  SECTIONS,
  SORTS,
  toSearchParams,
  toggleFacet,
  type CatalogQuery,
  type Mode,
  type Sort,
} from "@/lib/catalog-query";
import { sectionLabel } from "@/lib/wayfinding";

function useNavigate() {
  const router = useRouter();
  const [pending, startTransition] = React.useTransition();
  const go = React.useCallback(
    (q: CatalogQuery) => {
      const qs = toSearchParams(q).toString();
      startTransition(() => router.push(qs ? `/catalog?${qs}` : "/catalog"));
    },
    [router],
  );
  return { go, pending };
}

/** Search box + Advanced mode disclosure + sort (URL is the state). */
export function CatalogSearchBar({ query }: { query: CatalogQuery }) {
  const t = useTranslations("catalog");
  const { go, pending } = useNavigate();
  const [text, setText] = React.useState(query.q);
  React.useEffect(() => setText(query.q), [query.q]);
  const [advanced, setAdvanced] = React.useState(query.mode !== "hybrid");
  React.useEffect(() => {
    if (query.mode !== "hybrid") setAdvanced(true);
  }, [query.mode]);

  return (
    <div className="flex flex-col gap-4">
      <form
        role="search"
        aria-label={t("search.label")}
        onSubmit={(e) => {
          e.preventDefault();
          go({ ...query, q: text.trim(), page: 1 });
        }}
        className="flex gap-2"
      >
        <div className="relative flex-1">
          <Search
            className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-outline"
            aria-hidden
          />
          <Input
            type="search"
            name="q"
            data-testid="catalog-search-input"
            aria-label={t("search.label")}
            placeholder={t("search.placeholder")}
            className="pl-10 pr-20"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <div className="absolute right-2 top-1/2 flex -translate-y-1/2 items-center gap-0.5">
            {text && (
              <button
                type="button"
                aria-label={t("search.clear")}
                onClick={() => {
                  setText("");
                  go({ ...query, q: "", page: 1 });
                }}
                className="rounded p-1 text-outline hover:text-on-surface"
              >
                <X className="h-4 w-4" />
              </button>
            )}
            <button
              type="button"
              data-testid="catalog-scan"
              aria-label={t("search.scan")}
              title={t("search.scan")}
              onClick={() => openScanner("patron")}
              className="rounded p-1 text-outline hover:text-on-surface"
            >
              <ScanLine className="h-4 w-4" />
            </button>
          </div>
        </div>
        <Button type="submit" size="lg" disabled={pending} data-testid="catalog-search-submit">
          {t("search.submit")}
        </Button>
      </form>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-2">
          <button
            type="button"
            data-testid="catalog-advanced"
            aria-expanded={advanced}
            aria-controls="catalog-advanced-panel"
            onClick={() => setAdvanced((v) => !v)}
            className="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline"
          >
            {t("search.advanced")}
            <ChevronDown
              className={`h-3.5 w-3.5 transition-transform ${advanced ? "rotate-180" : ""}`}
              aria-hidden
            />
          </button>
          {advanced && (
            <div id="catalog-advanced-panel" data-testid="catalog-advanced-panel">
              <Tabs
                value={query.mode}
                onValueChange={(v) => go({ ...query, q: text.trim(), mode: v as Mode, page: 1 })}
              >
                <TabsList aria-label={t("modes.label")} className="w-auto">
                  {MODES.map((m) => (
                    <TabsTrigger key={m} value={m} data-testid={`mode-${m}`}>
                      {t(`modes.${m}`)}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
            </div>
          )}
        </div>
        <label className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          {t("sort.label")}
          <select
            data-testid="catalog-sort"
            value={query.sort}
            onChange={(e) => go({ ...query, sort: e.target.value as Sort, page: 1 })}
            className="h-8 rounded-md border border-border bg-card px-2 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {SORTS.map((s) => (
              <option key={s} value={s}>
                {t(`sort.${s}`)}
              </option>
            ))}
          </select>
        </label>
      </div>
    </div>
  );
}

/** Faceted wayfinding sidebar: sections (color dots), availability ("In Building"), era. */
export function FacetSidebar({ query }: { query: CatalogQuery }) {
  const t = useTranslations("catalog.filters");
  const tWay = useTranslations("wayfinding");
  const { go } = useNavigate();
  const active = query.sections.length + query.eras.length + (query.availableOnly ? 1 : 0);

  return (
    <aside aria-label={t("title")} className="flex flex-col gap-6" data-testid="facet-sidebar">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">{t("title")}</h2>
        {active > 0 && (
          <button
            type="button"
            data-testid="clear-filters"
            onClick={() => go({ ...DEFAULT_QUERY, q: query.q, mode: query.mode, sort: query.sort })}
            className="text-xs font-medium text-primary hover:underline"
          >
            {t("clear")}
          </button>
        )}
      </div>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {t("sections")}
        </legend>
        {SECTIONS.map((s) => (
          <label key={s.code} className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              data-testid={`facet-section-${s.code}`}
              checked={query.sections.includes(s.code)}
              onChange={() => go(toggleFacet(query, "sections", s.code))}
              className="h-4 w-4 accent-primary"
            />
            <span
              className="h-2.5 w-2.5 rounded-full"
              style={{ backgroundColor: s.color }}
              aria-hidden
            />
            <span>{sectionLabel(tWay, s.code, s.name)}</span>
          </label>
        ))}
      </fieldset>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {t("availability")}
        </legend>
        <label className="flex cursor-pointer items-start gap-2 text-sm">
          <input
            type="checkbox"
            data-testid="facet-available"
            checked={query.availableOnly}
            onChange={() => go({ ...query, availableOnly: !query.availableOnly, page: 1 })}
            className="mt-0.5 h-4 w-4 accent-primary"
          />
          <span className="flex flex-col">
            <span>{t("onlyAvailable")}</span>
            <span className="text-xs text-muted-foreground">{t("onlyAvailableHint")}</span>
          </span>
        </label>
      </fieldset>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {t("era")}
        </legend>
        {ERAS.map((e) => (
          <label key={e} className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              data-testid={`facet-era-${e}`}
              checked={query.eras.includes(e)}
              onChange={() => go(toggleFacet(query, "eras", e))}
              className="h-4 w-4 accent-primary"
            />
            <span>{t(`eras.${e}`)}</span>
          </label>
        ))}
      </fieldset>
    </aside>
  );
}
