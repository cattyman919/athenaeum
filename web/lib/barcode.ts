// Barcode intake helpers (docs/11): EAN-13/ISBN-13 pre-check mirroring the API's
// catalog.ValidateISBN, plus camera error classification for the §8 error table.
// Pure functions only — unit-tested in node.

/** Strips the separators people type or print inside ISBNs. */
export function normalizeIsbn(raw: string): string {
  return raw.replace(/[\s-]/g, "");
}

/** EAN-13 check digit for the first 12 digits, or null for malformed input. */
export function ean13CheckDigit(digits12: string): number | null {
  if (!/^\d{12}$/.test(digits12)) return null;
  let sum = 0;
  for (let i = 0; i < 12; i++) sum += Number(digits12[i]) * (i % 2 === 1 ? 3 : 1);
  return (10 - (sum % 10)) % 10;
}

export function isValidEan13(code: string): boolean {
  return /^\d{13}$/.test(code) && ean13CheckDigit(code.slice(0, 12)) === Number(code[12]);
}

/** ISBN-13 = checksum-valid EAN-13 with the Bookland 978/979 prefix. */
export function isValidIsbn13(code: string): boolean {
  return isValidEan13(code) && (code.startsWith("978") || code.startsWith("979"));
}

export type BarcodeVerdict =
  | { kind: "isbn"; isbn: string }
  | { kind: "upc" } // 12-digit store barcode (docs/11 §8)
  | { kind: "not_isbn" } // valid EAN-13 outside 978/979
  | { kind: "invalid" }; // checksum or shape failure

/** Classifies a scanned or typed code before calling POST /api/books/fetch-isbn. */
export function classifyBarcode(raw: string): BarcodeVerdict {
  const code = normalizeIsbn(raw);
  if (/^\d{12}$/.test(code)) return { kind: "upc" };
  if (!isValidEan13(code)) return { kind: "invalid" };
  if (!isValidIsbn13(code)) return { kind: "not_isbn" };
  return { kind: "isbn", isbn: code };
}

export type CameraErrorKind = "permission" | "no_device" | "insecure" | "wasm" | "unknown";

/** Maps getUserMedia / detector failures onto the docs/11 §8 scenarios. */
export function cameraErrorKind(err: unknown): CameraErrorKind {
  const name =
    typeof err === "object" && err !== null && "name" in err ? String(err.name) : undefined;
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
    case "PermissionDeniedError":
      return "permission";
    case "NotFoundError":
    case "OverconstrainedError":
    case "DevicesNotFoundError":
      return "no_device";
    case "InsecureContextError":
      return "insecure";
    case "DetectorLoadError":
      return "wasm";
    default:
      return "unknown";
  }
}

/** Error raised when getUserMedia is absent (insecure origin, docs/11 §5). */
export class InsecureContextError extends Error {
  override name = "InsecureContextError";
}

/** Error raised when the BarcodeDetector (native or WASM polyfill) cannot load. */
export class DetectorLoadError extends Error {
  override name = "DetectorLoadError";
}
