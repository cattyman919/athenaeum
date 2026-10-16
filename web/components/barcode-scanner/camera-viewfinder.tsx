"use client";

// Camera viewfinder for Patron Lookup (docs/11 §7): 4:3 → 16:10 surface, corner-bracket 3:2
// scan region with a dimmed surround, decorative HUD, torch/flip controls and the §8 camera
// error cards. Decode loop: hooks/use-barcode-scanner.ts (1.5 s cooldown, EAN-13 + Code 128).

import * as React from "react";
import { useTranslations } from "next-intl";
import { Camera, Flashlight, FlashlightOff, RefreshCw, SwitchCamera } from "lucide-react";
import { Button } from "@/components/ui/button";
import { classifyScanCode, type RejectedScan } from "@/lib/barcode";
import { UNIVERSAL_SCAN_FORMATS } from "@/lib/barcode-detector";
import { useBarcodeScanner } from "@/hooks/use-barcode-scanner";
import { cn } from "@/lib/utils";

export type ViewfinderFlash = "ok" | "reject" | null;

export interface CameraViewfinderProps {
  active: boolean;
  onDetected: (code: string, format?: string) => void;
  onRejected: (verdict: RejectedScan) => void;
  /** Focuses the manual code input (fallback path for every camera error). */
  onUseManual: () => void;
  flash: ViewfinderFlash;
}

const CORNERS = [
  "-left-1 -top-1 border-l-2 border-t-2 rounded-tl",
  "-right-1 -top-1 border-r-2 border-t-2 rounded-tr",
  "-bottom-1 -left-1 border-b-2 border-l-2 rounded-bl",
  "-bottom-1 -right-1 border-b-2 border-r-2 rounded-br",
] as const;

export function CameraViewfinder({
  active,
  onDetected,
  onRejected,
  onUseManual,
  flash,
}: CameraViewfinderProps) {
  const t = useTranslations("scanner.camera");
  const scanner = useBarcodeScanner({
    active,
    formats: UNIVERSAL_SCAN_FORMATS,
    classify: classifyScanCode,
    onDetected,
    onRejected,
  });
  const errorKind = scanner.status === "error" ? scanner.error : null;

  return (
    <div
      className="relative flex aspect-[4/3] w-full flex-col justify-between overflow-hidden rounded-lg bg-inverse-surface p-4 shadow-lg sm:aspect-[16/10]"
      data-testid="scanner-viewfinder"
      data-status={scanner.status}
    >
      <video
        ref={scanner.videoRef}
        className={cn(
          "absolute inset-0 h-full w-full object-cover",
          scanner.facingMode === "user" && "-scale-x-100",
        )}
        playsInline
        muted
        autoPlay
        aria-label={t("preview")}
      />

      {!errorKind && (
        <>
          <div
            className="relative z-10 flex items-center justify-between gap-2 font-mono text-[11px] uppercase tracking-wide text-white"
            aria-hidden
          >
            <span className="flex items-center gap-1.5">
              <span className="relative flex h-2.5 w-2.5">
                {scanner.status === "scanning" && (
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-status-available-dot opacity-75" />
                )}
                <span
                  className={cn(
                    "relative inline-flex h-2.5 w-2.5 rounded-full",
                    scanner.status === "scanning" ? "bg-status-available-dot" : "bg-white/50",
                  )}
                />
              </span>
              {scanner.status === "scanning" ? t("active") : t("starting")}
            </span>
            <span className="rounded bg-white/15 px-2 py-0.5 backdrop-blur-sm">
              EAN-13 · CODE 128 · {t("decoder")}
            </span>
          </div>

          <div className="pointer-events-none relative z-10 flex w-4/5 max-w-sm items-center justify-center self-center">
            <div
              data-testid="scanner-region"
              data-flash={flash ?? undefined}
              className={cn(
                "relative flex aspect-[3/2] w-full items-center justify-center rounded-md shadow-[0_0_0_9999px_rgba(0,0,0,0.35)] transition",
                flash === "ok" && "ring-4 ring-success",
                flash === "reject" && "ring-4 ring-destructive motion-safe:animate-shake",
              )}
            >
              {CORNERS.map((pos) => (
                <span
                  key={pos}
                  className={cn("absolute h-6 w-6 border-status-available-dot", pos)}
                  aria-hidden
                />
              ))}
              <span
                className="absolute inset-x-2 top-1/2 h-0.5 animate-pulse bg-gradient-to-r from-transparent via-status-available-dot to-transparent"
                aria-hidden
              />
              <span className="rounded bg-black/60 px-2 py-0.5 font-mono text-[11px] text-white/80 backdrop-blur-sm">
                {t("align")}
              </span>
            </div>
          </div>

          <div className="relative z-10 flex items-center gap-2">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="bg-white/15 text-white backdrop-blur-sm hover:bg-white/30 hover:text-white"
              onClick={scanner.flipCamera}
              aria-label={t("flip")}
              data-testid="scanner-flip"
            >
              <SwitchCamera className="h-4 w-4" aria-hidden />
            </Button>
            {scanner.torchSupported && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="bg-white/15 text-white backdrop-blur-sm hover:bg-white/30 hover:text-white"
                onClick={() => void scanner.toggleTorch()}
                aria-label={scanner.torchOn ? t("torchOff") : t("torchOn")}
                aria-pressed={scanner.torchOn}
                data-testid="scanner-torch"
              >
                {scanner.torchOn ? (
                  <FlashlightOff className="h-4 w-4" aria-hidden />
                ) : (
                  <Flashlight className="h-4 w-4" aria-hidden />
                )}
              </Button>
            )}
          </div>
        </>
      )}

      {scanner.status === "starting" && (
        <div className="absolute inset-0 z-0 flex items-center justify-center text-sm text-white">
          <Camera className="mr-2 h-4 w-4 animate-pulse" aria-hidden /> {t("starting")}
        </div>
      )}

      {errorKind && (
        <div
          role="alert"
          data-testid="scanner-camera-error"
          data-kind={errorKind}
          className="absolute inset-0 z-20 flex flex-col items-center justify-center gap-3 bg-surface-container p-6 text-center"
        >
          <p className="font-medium">{t(`errors.${errorKind}.title`)}</p>
          <p className="max-w-sm text-sm text-on-surface-variant">
            {t(`errors.${errorKind}.body`)}
          </p>
          <div className="flex gap-2">
            {(errorKind === "wasm" || errorKind === "unknown" || errorKind === "permission") && (
              <Button size="sm" variant="outline" onClick={scanner.retry}>
                <RefreshCw className="h-4 w-4" aria-hidden /> {t("retry")}
              </Button>
            )}
            <Button size="sm" onClick={onUseManual}>
              {t("useManual")}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
