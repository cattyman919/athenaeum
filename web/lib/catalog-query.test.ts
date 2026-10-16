import { describe, expect, it } from "vitest";
import {
  DEFAULT_QUERY,
  parseCatalogQuery,
  requiresQuery,
  toApiParams,
  toSearchParams,
  toggleFacet,
} from "./catalog-query";

describe("catalog URL state (docs/07 §5)", () => {
  it("parses defaults from empty params", () => {
    expect(parseCatalogQuery({})).toEqual(DEFAULT_QUERY);
  });

  it("parses every facet", () => {
    const q = parseCatalogQuery({
      q: " dune ",
      mode: "hybrid",
      section: ["SF", "REF"],
      availability: "available",
      era: ["2000_n"],
      sort: "year:desc",
      page: "3",
    });
    expect(q).toEqual({
      q: "dune",
      mode: "hybrid",
      sections: ["SF", "REF"],
      availableOnly: true,
      eras: ["2000_n"],
      sort: "year:desc",
      page: 3,
    });
  });

  it("drops unknown or malformed values instead of throwing", () => {
    const q = parseCatalogQuery({
      mode: "fuzzy",
      section: ["ZZ", "SF"],
      era: "medieval",
      sort: "popularity",
      page: "-4",
      availability: "maybe",
    });
    expect(q).toEqual({ ...DEFAULT_QUERY, sections: ["SF"] });
    expect(parseCatalogQuery({ page: "abc" }).page).toBe(1);
  });

  it("round-trips through the URL and omits defaults", () => {
    expect(toSearchParams(DEFAULT_QUERY).toString()).toBe("");
    const original = {
      q: "space opera",
      mode: "semantic" as const,
      sections: ["SF"],
      availableOnly: true,
      eras: ["1970_2000", "2000_n"],
      sort: "title" as const,
      page: 2,
    };
    const qs = toSearchParams(original).toString();
    const back = parseCatalogQuery(
      Object.fromEntries(
        [...new URLSearchParams(qs)].map(([k]) => [k, new URLSearchParams(qs).getAll(k)]),
      ),
    );
    expect(back).toEqual(original);
  });

  it("API params always carry mode and per_page", () => {
    const p = toApiParams({ ...DEFAULT_QUERY, q: "x" }, 12);
    expect(p.get("mode")).toBe("keyword");
    expect(p.get("per_page")).toBe("12");
    expect(p.get("q")).toBe("x");
  });

  it("requiresQuery gates semantic/hybrid but not keyword (docs/03 §5.1)", () => {
    expect(requiresQuery("keyword")).toBe(false);
    expect(requiresQuery("semantic")).toBe(true);
    expect(requiresQuery("hybrid")).toBe(true);
  });

  it("toggleFacet adds/removes and resets paging", () => {
    const a = toggleFacet({ ...DEFAULT_QUERY, page: 4 }, "sections", "SF");
    expect(a.sections).toEqual(["SF"]);
    expect(a.page).toBe(1);
    expect(toggleFacet(a, "sections", "SF").sections).toEqual([]);
  });
});
