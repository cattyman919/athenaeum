import { describe, expect, it } from "vitest";
import { fromAcceptLanguage, isLocale, resolveLocale, whisperLanguage } from "./locale";

describe("resolveLocale (docs/14 §12)", () => {
  it("prefers a valid cookie over Accept-Language", () => {
    expect(resolveLocale({ cookie: "zh-CN", acceptLanguage: "en-US,en;q=0.9" })).toBe("zh-CN");
    expect(resolveLocale({ cookie: "en", acceptLanguage: "zh-CN" })).toBe("en");
  });

  it("falls back to Accept-Language when the cookie is missing or invalid", () => {
    expect(resolveLocale({ acceptLanguage: "zh-CN,zh;q=0.9,en;q=0.8" })).toBe("zh-CN");
    expect(resolveLocale({ cookie: "fr", acceptLanguage: "zh-CN" })).toBe("zh-CN");
    expect(resolveLocale({ cookie: "zh-TW", acceptLanguage: "en-GB" })).toBe("en");
    expect(resolveLocale({ cookie: "", acceptLanguage: "zh" })).toBe("zh-CN");
  });

  it("falls back to en when nothing matches", () => {
    expect(resolveLocale({})).toBe("en");
    expect(resolveLocale({ cookie: null, acceptLanguage: null })).toBe("en");
    expect(resolveLocale({ acceptLanguage: "fr-FR,de;q=0.8" })).toBe("en");
    expect(resolveLocale({ acceptLanguage: "*" })).toBe("en");
  });
});

describe("fromAcceptLanguage", () => {
  it("honours q-weights", () => {
    expect(fromAcceptLanguage("en;q=0.4,zh-CN;q=0.9")).toBe("zh-CN");
    expect(fromAcceptLanguage("zh-CN;q=0.2,en;q=0.9")).toBe("en");
  });

  it("maps Simplified variants and skips deferred zh-TW/zh-HK", () => {
    expect(fromAcceptLanguage("zh-Hans")).toBe("zh-CN");
    expect(fromAcceptLanguage("zh-SG")).toBe("zh-CN");
    expect(fromAcceptLanguage("zh-TW")).toBeNull();
    expect(fromAcceptLanguage("zh-HK,en;q=0.5")).toBe("en");
  });

  it("ignores q=0 and malformed entries", () => {
    expect(fromAcceptLanguage("zh-CN;q=0,en;q=0.1")).toBe("en");
    expect(fromAcceptLanguage(";;;,,")).toBeNull();
    expect(fromAcceptLanguage(undefined)).toBeNull();
  });
});

describe("helpers", () => {
  it("isLocale only accepts the two tags", () => {
    expect(isLocale("en")).toBe(true);
    expect(isLocale("zh-CN")).toBe(true);
    expect(isLocale("zh-cn")).toBe(false);
    expect(isLocale(undefined)).toBe(false);
  });

  it("maps locale to the Whisper hint", () => {
    expect(whisperLanguage("zh-CN")).toBe("zh");
    expect(whisperLanguage("en")).toBe("en");
  });
});
