"use client";

// React wrapper over lib/barcode-scanner.ts (docs/11 §5–§7): owns the controller, starts
// it while `active`, and stops tracks + the frame loop on pause/unmount/dialog close.

import * as React from "react";
import type { BarcodeVerdict, CameraErrorKind } from "@/lib/barcode";
import { createBarcodeDetector } from "@/lib/barcode-detector";
import {
  createScanner,
  type FacingMode,
  type ScannerController,
  type StreamLike,
} from "@/lib/barcode-scanner";

export type ScannerStatus = "idle" | "starting" | "scanning" | "error";

export interface UseBarcodeScannerOptions {
  active: boolean;
  onDetected: (isbn: string) => void;
  onRejected?: (verdict: Exclude<BarcodeVerdict, { kind: "isbn" }>) => void;
  formats?: readonly string[];
}

export function useBarcodeScanner({
  active,
  onDetected,
  onRejected,
  formats,
}: UseBarcodeScannerOptions) {
  const videoRef = React.useRef<HTMLVideoElement | null>(null);
  const [status, setStatus] = React.useState<ScannerStatus>("idle");
  const [error, setError] = React.useState<CameraErrorKind | null>(null);
  const [facingMode, setFacingMode] = React.useState<FacingMode>("environment");
  const [torchSupported, setTorchSupported] = React.useState(false);
  const [torchOn, setTorchOn] = React.useState(false);
  const [attempt, setAttempt] = React.useState(0);

  // Latest callbacks without restarting the camera on every parent render.
  const handlers = React.useRef({ onDetected, onRejected });
  handlers.current = { onDetected, onRejected };
  const formatsKey = (formats ?? []).join(",");

  const controllerRef = React.useRef<ScannerController | null>(null);

  React.useEffect(() => {
    const video = videoRef.current;
    if (!active || !video) return;
    const md = typeof navigator !== "undefined" ? navigator.mediaDevices : undefined;
    const controller = createScanner(
      {
        getUserMedia: md?.getUserMedia
          ? (c) => md.getUserMedia(c) as Promise<StreamLike>
          : undefined,
        createDetector: () => createBarcodeDetector(formatsKey ? formatsKey.split(",") : undefined),
        requestFrame: (cb) => requestAnimationFrame(cb),
        cancelFrame: (id) => cancelAnimationFrame(id),
        now: () => performance.now(),
      },
      {
        onDetected: (isbn) => handlers.current.onDetected(isbn),
        onRejected: (v) => handlers.current.onRejected?.(v),
        onError: (kind) => {
          setError(kind);
          setStatus("error");
        },
        onStarted: ({ torch }) => {
          setTorchSupported(torch);
          setStatus("scanning");
        },
      },
    );
    controllerRef.current = controller;
    setError(null);
    setTorchOn(false);
    setStatus("starting");
    void controller.start(video, facingMode);
    return () => {
      controller.stop();
      video.srcObject = null;
      if (controllerRef.current === controller) controllerRef.current = null;
      setStatus("idle");
    };
  }, [active, facingMode, attempt, formatsKey]);

  const flipCamera = React.useCallback(
    () => setFacingMode((m) => (m === "environment" ? "user" : "environment")),
    [],
  );
  const retry = React.useCallback(() => setAttempt((n) => n + 1), []);
  const toggleTorch = React.useCallback(async () => {
    const next = !torchOn;
    if (await controllerRef.current?.setTorch(next)) setTorchOn(next);
  }, [torchOn]);

  return {
    videoRef,
    status,
    error,
    facingMode,
    flipCamera,
    retry,
    torchSupported,
    torchOn,
    toggleTorch,
  };
}
