import { describe, expect, it } from "vitest";
import { decideRoute } from "./route-guard";

describe("decideRoute (docs/07 §2)", () => {
  const cases: Array<[string, boolean, boolean, unknown]> = [
    ["/catalog", false, false, { action: "login", next: "/catalog" }],
    ["/books/42", false, false, { action: "login", next: "/books/42" }],
    ["/catalog", true, false, { action: "next" }],
    ["/catalog", true, true, { action: "next" }],
    ["/catalog", false, true, { action: "refresh" }],
    ["/login", false, false, { action: "next" }],
    ["/login", true, true, { action: "catalog" }],
    ["/login", true, false, { action: "catalog" }],
    ["/login", false, true, { action: "refresh" }],
    ["/admin", false, false, { action: "login", next: "/admin" }],
  ];
  for (const [pathname, hasAccess, hasRefresh, want] of cases) {
    it(`${pathname} access=${hasAccess} refresh=${hasRefresh}`, () => {
      expect(decideRoute({ pathname, hasAccess, hasRefresh })).toEqual(want);
    });
  }
});
