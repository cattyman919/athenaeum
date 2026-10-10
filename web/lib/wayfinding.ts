// Localized wayfinding vocabulary (docs/14 §6.1). The API returns structured location
// `parts` (docs/03 §5.2); the web composes the ladder, section chips and shelf lines from
// them via the `wayfinding.*` catalog keys. Codes and the path token are never translated;
// unknown floors/sections/tiers fall back to the raw English value from the API.

import type { LocationParts } from "@/lib/types";

export interface WayfindingTranslator {
  (key: string, values?: Record<string, string | number>): string;
  has(key: string): boolean;
}

/** Catalog slug for a `height_tier` value: "Eye Level" → "eye_level". */
export function tierSlug(tier: string): string {
  return tier
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

function lookup(t: WayfindingTranslator, key: string, fallback: string): string {
  return t.has(key) ? t(key) : fallback;
}

export function buildingLabel(t: WayfindingTranslator, code: string, fallback: string): string {
  return lookup(t, `building.${code}`, fallback);
}

export function floorLabel(t: WayfindingTranslator, n: number, fallback: string): string {
  return lookup(t, `floor.${n}`, fallback);
}

export function sectionLabel(t: WayfindingTranslator, code: string, fallback: string): string {
  return lookup(t, `section.${code}`, fallback);
}

export function tierLabel(t: WayfindingTranslator, tier: string): string {
  return lookup(t, `tier.${tierSlug(tier)}`, tier);
}

/** The four turn-by-turn steps: floor → section → aisle → shelf. */
export function ladderSteps(t: WayfindingTranslator, p: LocationParts): string[] {
  return [
    t("step.floor", { n: p.floor_number, name: floorLabel(t, p.floor_number, p.floor_name) }),
    t("step.section", { name: sectionLabel(t, p.section_code, p.section_name) }),
    t("step.aisle", { code: p.aisle_code }),
    t("step.shelf", { code: p.shelf_code, row: p.row_level, tier: tierLabel(t, p.height_tier) }),
  ];
}

/** Compact "Floor {n} • Shelf {code}" line (library rows, citation cards, copy manifest). */
export function shelfLine(
  t: WayfindingTranslator,
  p: Pick<LocationParts, "floor_number" | "shelf_code">,
): string {
  return t("shelfLine", { n: p.floor_number, code: p.shelf_code });
}
