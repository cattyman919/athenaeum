// BarcodeDetector loader (docs/11 §3): native implementation on Chromium/Android when it
// supports EAN-13, otherwise the `barcode-detector` ZXing-C++ WASM ponyfill — the same
// interface either way. Browser-only; imported dynamically by the scanner. The WASM binary
// is served from the app's own static assets (pinned `zxing-wasm` = barcode-detector's dep).

import type { Detector } from "./barcode-scanner";

export const SCAN_FORMATS = ["ean_13"] as const;

/** Universal scanner profile: EAN-13 (ISBN) + Code 128 (ATH copy tag) — docs/11 §3. */
export const UNIVERSAL_SCAN_FORMATS = ["ean_13", "code_128"] as const;

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
  const [{ BarcodeDetector, prepareZXingModule }, { default: wasmUrl }] = await Promise.all([
    import("barcode-detector/ponyfill"),
    // Emitted by next.config's asset/resource rule → /_next/static/media/zxing_reader.<hash>.wasm.
    import("zxing-wasm/reader/zxing_reader.wasm"),
  ]);
  // Self-host the ZXing binary instead of the library's jsDelivr default (offline / CSP safe).
  prepareZXingModule({
    overrides: {
      locateFile: (path: string, prefix: string) =>
        path.endsWith(".wasm") ? wasmUrl : prefix + path,
    },
  });
  return new BarcodeDetector({ formats: [...formats] as never }) as unknown as Detector;
}
