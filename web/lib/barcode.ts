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

/** Athenaeum copy tag printed as Code 128 (docs/11 §3, §9). */
export const COPY_TAG_RE = /^ATH-\d{6}-C\d{2}$/;

/** Every verdict the universal scanner can reach (ISBN-13 or copy tag accepted). */
export type ScanVerdict = BarcodeVerdict | { kind: "copy_tag"; tag: string } | { kind: "isbn10" }; // ISBN-10 is unsupported in v1 (docs/11 §1, §8)

export type RejectedScan = Exclude<ScanVerdict, { kind: "isbn" } | { kind: "copy_tag" }>;

/**
 * Classifies a scanned or typed code before calling POST /api/books/scan (docs/03 §5.4):
 * copy tags are uppercased and shape-checked, everything else goes through the EAN-13 check.
 */
export function classifyScanCode(raw: string): ScanVerdict {
  const compact = raw.replace(/\s/g, "").toUpperCase();
  if (compact.startsWith("ATH")) {
    return COPY_TAG_RE.test(compact) ? { kind: "copy_tag", tag: compact } : { kind: "invalid" };
  }
  const code = normalizeIsbn(compact);
  if (/^\d{9}[\dX]$/.test(code)) return { kind: "isbn10" };
  return classifyBarcode(code);
}

/** The code to send for an accepted verdict, else null. */
export function acceptedCode(v: ScanVerdict): string | null {
  if (v.kind === "isbn") return v.isbn;
  if (v.kind === "copy_tag") return v.tag;
  return null;
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
