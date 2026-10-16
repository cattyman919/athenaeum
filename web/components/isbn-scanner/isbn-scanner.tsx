"use client";

// ISBNScanner (docs/11 §6–§8): camera surface + 3:2 scan region, torch/flip controls and
// the §8 error cards. Admin-only surface → English copy (docs/14 §1).

import * as React from "react";
import { Camera, Flashlight, FlashlightOff, RefreshCw, SwitchCamera } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { CameraErrorKind, RejectedScan } from "@/lib/barcode";
import { SCAN_FORMATS } from "@/lib/barcode-detector";
import { useBarcodeScanner } from "@/hooks/use-barcode-scanner";
import { cn } from "@/lib/utils";

export interface ScanRegion {
  /** Width of the target rectangle as a fraction of the video width. */
  widthPct: number;
  /** Width:height ratio — EAN codes sit flat, so wide (docs/11 §7). */
  aspect: number;
}

export interface ISBNScannerProps {
  onDetected: (isbn: string) => void;
  /** Switches the dialog to manual ISBN entry (fallback path, docs/11 §2). */
  onUseManual?: () => void;
  formats?: readonly string[];
  region?: ScanRegion;
  active?: boolean;
}

const ERROR_COPY: Record<CameraErrorKind, { title: string; body: string }> = {
  permission: {
    title: "Camera access was denied",
    body: "Allow camera access in your browser's site settings, or type the ISBN instead.",
  },
  no_device: {
    title: "Connect a camera",
    body: "No camera was found on this device. Use manual ISBN entry instead.",
  },
  insecure: {
    title: "Camera requires HTTPS",
    body: "Browsers only expose the camera on secure origins. Use manual ISBN entry.",
  },
  wasm: {
    title: "Barcode reader failed to load",
    body: "The scanner engine could not be downloaded. Retry or type the ISBN.",
  },
  unknown: {
    title: "Camera unavailable",
    body: "The camera could not be started. Retry or type the ISBN.",
  },
};

const REJECT_COPY: Record<RejectedScan["kind"], string> = {
  invalid: "Barcode unusable — scan a different surface.",
  isbn10: "ISBN-10 is not supported — scan the ISBN-13/EAN-13 barcode instead.",
  upc: "That is the store (UPC) barcode — scan the ISBN/EAN-13 barcode instead.",
  not_isbn: "That EAN-13 is not an ISBN (978/979) — scan the book's ISBN barcode.",
};

/** Haptics + 880 Hz / 40 ms confirmation beep (docs/11 §7). Best effort. */
export function scanFeedback() {
  try {
    navigator.vibrate?.(50);
  } catch {
    /* unsupported */
  }
  try {
    const Ctx =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctx) return;
    const ctx = new Ctx();
    const osc = ctx.createOscillator();
    osc.frequency.value = 880;
    osc.connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + 0.04);
    osc.onended = () => void ctx.close();
  } catch {
    /* audio blocked */
  }
}

export function ISBNScanner({
  onDetected,
  onUseManual,
  formats = SCAN_FORMATS,
  region = { widthPct: 0.8, aspect: 3 / 2 },
  active = true,
}: ISBNScannerProps) {
  const [flash, setFlash] = React.useState<"ok" | "reject" | null>(null);
  const [rejectMsg, setRejectMsg] = React.useState<string | null>(null);

  const scanner = useBarcodeScanner({
    active,
    formats,
    onDetected: (isbn) => {
      scanFeedback();
      setRejectMsg(null);
      setFlash("ok");
      onDetected(isbn);
    },
    onRejected: (v) => {
      setRejectMsg(REJECT_COPY[v.kind]);
      setFlash("reject");
    },
  });

  React.useEffect(() => {
    if (!flash) return;
    const id = setTimeout(() => setFlash(null), 600);
    return () => clearTimeout(id);
  }, [flash]);

  const err = scanner.status === "error" && scanner.error ? ERROR_COPY[scanner.error] : null;

  return (
    <div className="space-y-3" data-testid="isbn-scanner" data-status={scanner.status}>
      <p className="text-sm text-on-surface-variant">
        Hold the book&apos;s ISBN barcode inside the frame, 20–30 cm from the lens.{" "}
        {onUseManual && (
          <>
            No camera access?{" "}
            <button type="button" className="font-medium underline" onClick={onUseManual}>
              Enter the ISBN manually
            </button>
          </>
        )}
      </p>

      <div className="relative aspect-video w-full overflow-hidden rounded-lg bg-black">
        <video
          ref={scanner.videoRef}
          className={cn(
            "h-full w-full object-cover",
            scanner.facingMode === "user" && "-scale-x-100",
          )}
          playsInline
          muted
          autoPlay
          aria-label="Camera preview"
        />
        {!err && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/40">
            <div
              data-testid="scan-region"
              className={cn(
                "relative rounded-md shadow-[0_0_0_9999px_rgba(0,0,0,0.35)] transition",
                flash === "ok" && "ring-4 ring-success",
                flash === "reject" && "animate-pulse ring-4 ring-destructive",
              )}
              style={{ width: `${region.widthPct * 100}%`, aspectRatio: String(region.aspect) }}
            >
              {(
                [
                  "left-0 top-0 border-l-4 border-t-4",
                  "right-0 top-0 border-r-4 border-t-4",
                  "bottom-0 left-0 border-b-4 border-l-4",
                  "bottom-0 right-0 border-b-4 border-r-4",
                ] as const
              ).map((pos) => (
                <span key={pos} className={cn("absolute h-6 w-6 rounded-sm border-white", pos)} />
              ))}
            </div>
          </div>
        )}
        {scanner.status === "starting" && (
          <div className="absolute inset-0 flex items-center justify-center text-sm text-white">
            <Camera className="mr-2 h-4 w-4 animate-pulse" aria-hidden /> Starting camera…
          </div>
        )}
        {err && (
          <div
            role="alert"
            data-testid="scanner-error"
            data-kind={scanner.error ?? undefined}
            className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-surface-container p-6 text-center"
          >
            <p className="font-medium">{err.title}</p>
            <p className="text-sm text-on-surface-variant">{err.body}</p>
            <div className="flex gap-2">
              {(scanner.error === "wasm" ||
                scanner.error === "unknown" ||
                scanner.error === "permission") && (
                <Button size="sm" variant="outline" onClick={scanner.retry}>
                  <RefreshCw className="h-4 w-4" aria-hidden /> Retry
                </Button>
              )}
              {onUseManual && (
                <Button size="sm" onClick={onUseManual}>
                  Enter ISBN manually
                </Button>
              )}
            </div>
          </div>
        )}
      </div>

      {rejectMsg && (
        <p className="text-sm text-destructive" role="status" data-testid="scan-reject">
          {rejectMsg}
        </p>
      )}

      <div className="flex gap-2">
        <Button
          size="sm"
          variant="outline"
          onClick={scanner.flipCamera}
          aria-label="Switch camera"
          disabled={scanner.status === "error"}
        >
          <SwitchCamera className="h-4 w-4" aria-hidden />
        </Button>
        {scanner.torchSupported && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => void scanner.toggleTorch()}
            aria-label={scanner.torchOn ? "Turn torch off" : "Turn torch on"}
            aria-pressed={scanner.torchOn}
          >
            {scanner.torchOn ? (
              <FlashlightOff className="h-4 w-4" aria-hidden />
            ) : (
              <Flashlight className="h-4 w-4" aria-hidden />
            )}
          </Button>
        )}
      </div>
    </div>
  );
}
