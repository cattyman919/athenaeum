import { describe, expect, it } from "vitest";
import en from "./en.json";
import zh from "./zh-CN.json";
import { ENUM_VALUES, enumLabel } from "@/lib/enum-labels";

type Tree = { [key: string]: string | Tree };

function flatten(tree: Tree, prefix = ""): string[] {
  return Object.entries(tree).flatMap(([k, v]) =>
    typeof v === "string" ? [`${prefix}${k}`] : flatten(v, `${prefix}${k}.`),
  );
}

function lookup(tree: Tree, path: string): string | undefined {
  let cur: string | Tree | undefined = tree;
  for (const part of path.split(".")) {
    if (typeof cur !== "object" || cur === null) return undefined;
    cur = cur[part];
  }
  return typeof cur === "string" ? cur : undefined;
}

describe("catalog parity (docs/14 §12)", () => {
  it("en and zh-CN have identical key sets", () => {
    const enKeys = flatten(en as Tree).sort();
    const zhKeys = flatten(zh as Tree).sort();
    expect(zhKeys.filter((k) => !enKeys.includes(k))).toEqual([]);
    expect(enKeys.filter((k) => !zhKeys.includes(k))).toEqual([]);
  });

  it("no empty strings and matching ICU placeholders", () => {
    for (const key of flatten(en as Tree)) {
      const a = lookup(en as Tree, key)!;
      const b = lookup(zh as Tree, key)!;
      expect(a.trim(), key).not.toBe("");
      expect(b.trim(), key).not.toBe("");
      const ph = (s: string) =>
        [...new Set([...s.matchAll(/\{(\w+)[,}]/g)].map((m) => m[1]))].sort();
      expect(ph(b), `placeholders of ${key}`).toEqual(ph(a));
    }
  });
});

function translator(catalog: Tree, ns: string) {
  const t = ((key: string) => lookup(catalog, `${ns}.${key}`) ?? key) as ((
    key: string,
  ) => string) & {
    has(key: string): boolean;
  };
  t.has = (key: string) => lookup(catalog, `${ns}.${key}`) !== undefined;
  return t;
}

describe("enum label maps (docs/14 §12)", () => {
  for (const [name, catalog] of [
    ["en", en],
    ["zh-CN", zh],
  ] as const) {
    it(`every enum value has a ${name} label`, () => {
      const t = translator(catalog as Tree, "enums");
      for (const [kind, values] of Object.entries(ENUM_VALUES)) {
        for (const v of values) {
          expect(t.has(`${kind}.${v}`), `${kind}.${v}`).toBe(true);
          expect(enumLabel(t, kind as keyof typeof ENUM_VALUES, v).length).toBeGreaterThan(0);
        }
      }
    });
  }

  it("unknown values fall back to the raw English string", () => {
    const t = translator(zh as Tree, "enums");
    expect(enumLabel(t, "availability", "teleported")).toBe("teleported");
  });

  it("zh labels differ from en for translated values", () => {
    const tz = translator(zh as Tree, "enums");
    expect(enumLabel(tz, "copyStatus", "checked_out")).toBe("已借出");
  });
});
