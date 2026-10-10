// Enum display-label maps (docs/14 §6). The API returns English enum values; the
// catalog (messages/*.json → `enums.*`) provides localized labels. One place to edit.

export const ENUM_VALUES = {
  availability: [
    "available",
    "checked_out",
    "not_in_building",
    "missing_lost",
    "reserved_waitlist",
  ],
  copyStatus: ["available", "checked_out", "reserved_waitlist", "missing_lost"],
  readingStage: ["reading", "read", "want_to_read", "favorites"],
} as const;

export type EnumKind = keyof typeof ENUM_VALUES;

interface Translator {
  (key: string): string;
  has(key: string): boolean;
}

/** Localized label for `value`; unknown values fall back to the raw English string. */
export function enumLabel(t: Translator, kind: EnumKind, value: string): string {
  const key = `${kind}.${value}`;
  return t.has(key) ? t(key) : value;
}
