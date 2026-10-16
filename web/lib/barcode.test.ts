import { describe, expect, it } from "vitest";
import {
  DetectorLoadError,
  InsecureContextError,
  acceptedCode,
  cameraErrorKind,
  classifyBarcode,
  classifyScanCode,
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

describe("classifyScanCode — universal scanner (docs/11 §8, §9)", () => {
  it.each([
    ["978-0-441-01359-3", { kind: "isbn", isbn: "9780441013593" }],
    ["ATH-002941-C01", { kind: "copy_tag", tag: "ATH-002941-C01" }],
    [" ath-002960-c01 ", { kind: "copy_tag", tag: "ATH-002960-C01" }],
    ["ATH-2941-C1", { kind: "invalid" }],
    ["0441013597", { kind: "isbn10" }],
    ["044101359X", { kind: "isbn10" }],
    ["036000291452", { kind: "upc" }],
    ["9780441013594", { kind: "invalid" }],
    ["4006381333931", { kind: "not_isbn" }],
    ["", { kind: "invalid" }],
  ] as const)("%s → %o", (raw, verdict) => {
    expect(classifyScanCode(raw)).toEqual(verdict);
  });

  it("acceptedCode returns the payload to send only for ISBNs and copy tags", () => {
    expect(acceptedCode({ kind: "isbn", isbn: "9780441013593" })).toBe("9780441013593");
    expect(acceptedCode({ kind: "copy_tag", tag: "ATH-002941-C01" })).toBe("ATH-002941-C01");
    expect(acceptedCode({ kind: "isbn10" })).toBeNull();
    expect(acceptedCode({ kind: "upc" })).toBeNull();
  });
});
