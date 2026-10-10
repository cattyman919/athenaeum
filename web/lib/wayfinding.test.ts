import { createTranslator } from "next-intl";
import { describe, expect, it } from "vitest";
import en from "@/messages/en.json";
import zh from "@/messages/zh-CN.json";
import type { LocationParts } from "@/lib/types";
import {
  buildingLabel,
  floorLabel,
  ladderSteps,
  sectionLabel,
  shelfLine,
  tierLabel,
  tierSlug,
  type WayfindingTranslator,
} from "@/lib/wayfinding";

// Building vocabulary from docs/13 (api/mocks/building.json).
const BUILDINGS = { MAIN: "Athenaeum Main Building" };
const FLOORS = { 1: "Reading Hall", 2: "West Stacks", 3: "North Stacks" };
const SECTIONS = {
  SCI: "Science, Mathematics & Tech",
  PER: "Periodicals & Archives",
  HVA: "History, Humanities & Arts",
  REF: "Reference & Special Folios",
  SF: "Science Fiction & Speculative",
};
const TIERS = ["Low Stacks", "Mid Stacks", "Eye Level", "Tower Top"];

const dune: LocationParts = {
  building_code: "MAIN",
  building_name: "Athenaeum Main Building",
  floor_number: 3,
  floor_name: "North Stacks",
  section_code: "SF",
  section_name: "Science Fiction & Speculative",
  aisle_code: "A-1",
  shelf_code: "S-2",
  row_level: 4,
  height_tier: "Eye Level",
};

function tr(locale: "en" | "zh-CN"): WayfindingTranslator {
  const t = createTranslator({
    locale,
    messages: locale === "en" ? en : zh,
    namespace: "wayfinding",
  }) as unknown as WayfindingTranslator;
  return t;
}

describe("wayfinding vocabulary (docs/14 §6.1, §12)", () => {
  for (const locale of ["en", "zh-CN"] as const) {
    it(`every building/floor/section/tier has a ${locale} entry`, () => {
      const t = tr(locale);
      for (const code of Object.keys(BUILDINGS)) expect(t.has(`building.${code}`), code).toBe(true);
      for (const n of Object.keys(FLOORS)) expect(t.has(`floor.${n}`), n).toBe(true);
      for (const code of Object.keys(SECTIONS)) expect(t.has(`section.${code}`), code).toBe(true);
      for (const tier of TIERS) expect(t.has(`tier.${tierSlug(tier)}`), tier).toBe(true);
    });
  }

  it("en entries equal the seeded English names", () => {
    const t = tr("en");
    for (const [code, name] of Object.entries(BUILDINGS))
      expect(buildingLabel(t, code, "?")).toBe(name);
    for (const [n, name] of Object.entries(FLOORS))
      expect(floorLabel(t, Number(n), "?")).toBe(name);
    for (const [code, name] of Object.entries(SECTIONS))
      expect(sectionLabel(t, code, "?")).toBe(name);
    for (const tier of TIERS) expect(tierLabel(t, tier)).toBe(tier);
  });

  it("tier slugs", () => {
    expect(tierSlug("Eye Level")).toBe("eye_level");
    expect(tierSlug(" Tower Top ")).toBe("tower_top");
  });

  it("en ladder matches the API prose", () => {
    expect(ladderSteps(tr("en"), dune)).toEqual([
      "Floor 3 — North Stacks",
      "Section: Science Fiction & Speculative",
      "Aisle A-1",
      "Shelf S-2 — Row 4, Eye Level",
    ]);
    expect(shelfLine(tr("en"), dune)).toBe("Floor 3 • Shelf S-2");
  });

  it("zh-CN ladder is Chinese with codes untouched", () => {
    expect(ladderSteps(tr("zh-CN"), dune)).toEqual([
      "3 楼 — 北区书库",
      "分区：科幻与奇幻",
      "A-1 通道",
      "书架 S-2 — 第 4 层，平视高度",
    ]);
    expect(shelfLine(tr("zh-CN"), dune)).toBe("3 楼 • 书架 S-2");
  });

  it("unknown floors/sections/tiers fall back to the raw API value", () => {
    const steps = ladderSteps(tr("zh-CN"), {
      ...dune,
      floor_number: 9,
      floor_name: "Roof Garden",
      section_code: "NEW",
      section_name: "New Arrivals",
      height_tier: "Basement Bin",
    });
    expect(steps[0]).toBe("9 楼 — Roof Garden");
    expect(steps[1]).toBe("分区：New Arrivals");
    expect(steps[3]).toBe("书架 S-2 — 第 4 层，Basement Bin");
  });
});
