// URL-as-state for /catalog (docs/07 §5): parse/serialize the search params shared by the
// page, the facet sidebar and the command palette.

export const SECTIONS = [
  // English names are the fallback; the UI shows `wayfinding.section.<code>` (docs/14 §6.1).
  { code: "SCI", name: "Science, Mathematics & Tech", color: "#10B981" },
  { code: "PER", name: "Periodicals & Archives", color: "#F59E0B" },
  { code: "HVA", name: "History, Humanities & Arts", color: "#8B5CF6" },
  { code: "REF", name: "Reference & Special Folios", color: "#6B7280" },
  { code: "SF", name: "Science Fiction & Speculative", color: "#3B82F6" },
] as const;

export const ERAS = ["pre1800", "1800_1970", "1970_2000", "2000_n"] as const;
export const MODES = ["keyword", "semantic", "hybrid"] as const;
export const SORTS = ["relevance", "title", "year:asc", "year:desc"] as const;

export type Mode = (typeof MODES)[number];
export type Sort = (typeof SORTS)[number];

export interface CatalogQuery {
  q: string;
  mode: Mode;
  sections: string[];
  availableOnly: boolean;
  eras: string[];
  sort: Sort;
  page: number;
}

export const DEFAULT_QUERY: CatalogQuery = {
  q: "",
  mode: "hybrid",
  sections: [],
  availableOnly: false,
  eras: [],
  sort: "relevance",
  page: 1,
};

type Raw = Record<string, string | string[] | undefined>;

function list(v: string | string[] | undefined): string[] {
  if (v === undefined) return [];
  return Array.isArray(v) ? v : [v];
}

function pick<T extends string>(
  v: string | string[] | undefined,
  allowed: readonly T[],
  fallback: T,
): T {
  const s = Array.isArray(v) ? v[0] : v;
  return (allowed as readonly string[]).includes(s ?? "") ? (s as T) : fallback;
}

/** Parses Next `searchParams`; unknown values fall back to defaults (never throws). */
export function parseCatalogQuery(raw: Raw): CatalogQuery {
  const page = Number.parseInt(
    String(Array.isArray(raw.page) ? raw.page[0] : (raw.page ?? "1")),
    10,
  );
  const sectionCodes: readonly string[] = SECTIONS.map((s) => s.code);
  return {
    q: (Array.isArray(raw.q) ? raw.q[0] : raw.q)?.trim() ?? "",
    mode: pick(raw.mode, MODES, "hybrid"),
    sections: list(raw.section).filter((s) => sectionCodes.includes(s)),
    availableOnly:
      (Array.isArray(raw.availability) ? raw.availability[0] : raw.availability) === "available",
    eras: list(raw.era).filter((e) => (ERAS as readonly string[]).includes(e)),
    sort: pick(raw.sort, SORTS, "relevance"),
    page: Number.isFinite(page) && page > 0 ? page : 1,
  };
}

/** Serializes to URLSearchParams, omitting defaults so shared links stay short. */
export function toSearchParams(q: Partial<CatalogQuery>): URLSearchParams {
  const p = new URLSearchParams();
  if (q.q) p.set("q", q.q);
  if (q.mode && q.mode !== "hybrid") p.set("mode", q.mode);
  for (const s of q.sections ?? []) p.append("section", s);
  if (q.availableOnly) p.set("availability", "available");
  for (const e of q.eras ?? []) p.append("era", e);
  if (q.sort && q.sort !== "relevance") p.set("sort", q.sort);
  if (q.page && q.page > 1) p.set("page", String(q.page));
  return p;
}

/** Query string for the API call (same params + explicit mode/per_page). */
export function toApiParams(q: CatalogQuery, perPage = 20): URLSearchParams {
  const p = toSearchParams({ ...q });
  p.set("mode", q.q === "" ? "keyword" : q.mode);
  p.set("per_page", String(perPage));
  return p;
}

/** Only an explicit semantic search needs a query; hybrid/keyword with an empty box browse. */
export function requiresQuery(mode: Mode): boolean {
  return mode === "semantic";
}

/** Toggles a value inside a multi-value facet and resets paging. */ export function toggleFacet(
  q: CatalogQuery,
  key: "sections" | "eras",
  value: string,
): CatalogQuery {
  const cur = q[key];
  const next = cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value];
  return { ...q, [key]: next, page: 1 };
}
