// BarcodeDetector loader (docs/11 §3): native implementation on Chromium/Android when it
// supports EAN-13, otherwise the `barcode-detector` ZXing-C++ WASM ponyfill — the same
// interface either way. Browser-only; imported dynamically by the scanner.

import type { Detector } from "./barcode-scanner";

export const SCAN_FORMATS = ["ean_13"] as const;

interface DetectorCtor {
  new (opts: { formats: string[] }): Detector;
  getSupportedFormats?: () => Promise<string[]>;
}

export async function createBarcodeDetector(
  formats: readonly string[] = SCAN_FORMATS,
): Promise<Detector> {
  const native = (globalThis as { BarcodeDetector?: DetectorCtor }).BarcodeDetector;
  if (native) {
    try {
      const supported = (await native.getSupportedFormats?.()) ?? [];
      if (formats.every((f) => supported.includes(f))) return new native({ formats: [...formats] });
    } catch {
      // fall through to the polyfill
    }
  }
  const { BarcodeDetector } = await import("barcode-detector/ponyfill");
  return new BarcodeDetector({ formats: [...formats] as never }) as unknown as Detector;
}
