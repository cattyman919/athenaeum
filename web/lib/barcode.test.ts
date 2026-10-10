import { describe, expect, it } from "vitest";
import {
  DetectorLoadError,
  InsecureContextError,
  cameraErrorKind,
  classifyBarcode,
  ean13CheckDigit,
  isValidEan13,
  isValidIsbn13,
  normalizeIsbn,
} from "./barcode";

describe("EAN-13 / ISBN-13 pre-check (docs/11 §9 mirror)", () => {
  it("computes check digits", () => {
    expect(ean13CheckDigit("978044101359")).toBe(3);
    expect(ean13CheckDigit("978055329335")).toBe(7);
    expect(ean13CheckDigit("12345")).toBeNull();
    expect(ean13CheckDigit("97804410135X")).toBeNull();
  });

  it("validates like the API's pkg/checkdigit", () => {
    for (const ok of ["9780441013593", "9780060850524", "9788100000021", "4006381333931"]) {
      expect(isValidEan13(ok), ok).toBe(true);
    }
    for (const bad of ["9780441013594", "978044101359", "97804410135930", "", "abcdefghijklm"]) {
      expect(isValidEan13(bad), bad).toBe(false);
    }
    expect(isValidIsbn13("4006381333931")).toBe(false);
    expect(isValidIsbn13("9780441013593")).toBe(true);
  });

  it("normalizes separators", () => {
    expect(normalizeIsbn(" 978-0-441-01359-3 ")).toBe("9780441013593");
  });

  it("classifies scanned codes", () => {
    expect(classifyBarcode("978-0-553-29335-7")).toEqual({ kind: "isbn", isbn: "9780553293357" });
    expect(classifyBarcode("036000291452")).toEqual({ kind: "upc" });
    expect(classifyBarcode("4006381333931")).toEqual({ kind: "not_isbn" });
    expect(classifyBarcode("9780553293358")).toEqual({ kind: "invalid" });
  });
});

describe("cameraErrorKind (docs/11 §8)", () => {
  const named = (name: string) => Object.assign(new Error(name), { name });
  it.each([
    ["NotAllowedError", "permission"],
    ["SecurityError", "permission"],
    ["NotFoundError", "no_device"],
    ["OverconstrainedError", "no_device"],
    ["AbortError", "unknown"],
  ] as const)("%s → %s", (name, kind) => {
    expect(cameraErrorKind(named(name))).toBe(kind);
  });

  it("maps the scanner's own errors", () => {
    expect(cameraErrorKind(new InsecureContextError("x"))).toBe("insecure");
    expect(cameraErrorKind(new DetectorLoadError("x"))).toBe("wasm");
    expect(cameraErrorKind("weird")).toBe("unknown");
  });
});
