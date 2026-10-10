// Camera scan loop (docs/11 §5) as a framework-free controller: getUserMedia →
// BarcodeDetector.detect() per new video frame → EAN-13 pre-check → cooldown-guarded
// callbacks. Every browser API is injected so the loop is unit-testable in node; the
// React wrapper lives in hooks/use-barcode-scanner.ts.

import {
  DetectorLoadError,
  InsecureContextError,
  cameraErrorKind,
  classifyBarcode,
  type BarcodeVerdict,
  type CameraErrorKind,
} from "./barcode";

export type FacingMode = "environment" | "user";

/** The subset of the Web BarcodeDetector interface the loop needs. */
export interface Detector {
  detect(source: unknown): Promise<Array<{ rawValue: string; format?: string }>>;
}

export interface VideoLike {
  srcObject: unknown;
  currentTime: number;
  play(): Promise<void> | void;
}

export interface TrackLike {
  stop(): void;
  getCapabilities?: () => object;
  applyConstraints?: (c: MediaTrackConstraints) => Promise<void>;
}

export interface StreamLike {
  getTracks(): TrackLike[];
  getVideoTracks(): TrackLike[];
}

export interface ScannerDeps {
  /** navigator.mediaDevices?.getUserMedia bound; undefined on insecure origins. */
  getUserMedia?: (c: MediaStreamConstraints) => Promise<StreamLike>;
  createDetector: () => Promise<Detector>;
  requestFrame: (cb: () => void) => number;
  cancelFrame: (id: number) => void;
  now: () => number;
}

export interface ScannerCallbacks {
  onDetected: (isbn: string) => void;
  /** Non-ISBN or checksum-failing reads (red shake / UPC hint, docs/11 §8). */
  onRejected?: (verdict: Exclude<BarcodeVerdict, { kind: "isbn" }>) => void;
  onError: (kind: CameraErrorKind, err: unknown) => void;
  onStarted?: (info: { torch: boolean }) => void;
}

/** 1.5 s lockout after a detection to prevent duplicate prefills (docs/11 §7). */
export const SCAN_COOLDOWN_MS = 1500;

export function cameraConstraints(facingMode: FacingMode): MediaStreamConstraints {
  return {
    video: { facingMode: { ideal: facingMode }, width: { ideal: 1920 } },
    audio: false,
  };
}

export interface ScannerController {
  start(video: VideoLike, facingMode?: FacingMode): Promise<void>;
  stop(): void;
  setTorch(on: boolean): Promise<boolean>;
  readonly running: boolean;
}

export function createScanner(deps: ScannerDeps, cb: ScannerCallbacks): ScannerController {
  let generation = 0; // bumps on every start/stop so stale async work bails out
  let stream: StreamLike | null = null;
  let rafId: number | null = null;
  let detector: Detector | null = null;
  let lastFrameTime = -1;
  let lockedUntil = 0;
  let busy = false;

  function release() {
    if (rafId !== null) deps.cancelFrame(rafId);
    rafId = null;
    stream?.getTracks().forEach((t) => t.stop());
    stream = null;
    lastFrameTime = -1;
    busy = false;
  }

  function fail(gen: number, err: unknown) {
    if (gen !== generation) return;
    release();
    generation++;
    cb.onError(cameraErrorKind(err), err);
  }

  async function tick(gen: number, video: VideoLike) {
    if (gen !== generation || !detector) return;
    if (!busy && video.currentTime !== lastFrameTime) {
      busy = true;
      lastFrameTime = video.currentTime;
      try {
        const hits = await detector.detect(video);
        if (gen !== generation) return;
        const first = hits[0];
        if (first && deps.now() >= lockedUntil) {
          const verdict = classifyBarcode(first.rawValue);
          lockedUntil = deps.now() + SCAN_COOLDOWN_MS;
          if (verdict.kind === "isbn") cb.onDetected(verdict.isbn);
          else cb.onRejected?.(verdict);
        }
      } catch (err) {
        // The WASM polyfill loads lazily on the first detect(): a failure here is the
        // "WASM load failure" scenario of docs/11 §8.
        fail(gen, new DetectorLoadError(err instanceof Error ? err.message : String(err)));
        return;
      } finally {
        busy = false;
      }
    }
    if (gen === generation) rafId = deps.requestFrame(() => void tick(gen, video));
  }

  return {
    get running() {
      return stream !== null;
    },

    async start(video, facingMode = "environment") {
      release();
      const gen = ++generation;
      if (!deps.getUserMedia) {
        fail(gen, new InsecureContextError("getUserMedia unavailable"));
        return;
      }
      try {
        detector ??= await deps.createDetector();
      } catch (err) {
        fail(gen, new DetectorLoadError(err instanceof Error ? err.message : String(err)));
        return;
      }
      let s: StreamLike;
      try {
        s = await deps.getUserMedia(cameraConstraints(facingMode));
      } catch (err) {
        fail(gen, err);
        return;
      }
      if (gen !== generation) {
        // Stopped (or restarted) while the permission prompt was open: StrictMode-safe.
        s.getTracks().forEach((t) => t.stop());
        return;
      }
      stream = s;
      video.srcObject = s;
      try {
        await video.play();
      } catch {
        // autoplay rejections are harmless for a muted inline <video>
      }
      const track = s.getVideoTracks()[0];
      const torch = hasTorch(track);
      cb.onStarted?.({ torch });
      rafId = deps.requestFrame(() => void tick(gen, video));
    },

    stop() {
      generation++;
      release();
    },

    async setTorch(on) {
      const track = stream?.getVideoTracks()[0];
      if (!track?.applyConstraints || !hasTorch(track)) return false;
      try {
        await track.applyConstraints({
          advanced: [{ torch: on } as unknown as MediaTrackConstraintSet],
        });
        return true;
      } catch {
        return false;
      }
    },
  };
}

function hasTorch(track: TrackLike | undefined): boolean {
  const caps = track?.getCapabilities?.() as { torch?: unknown } | undefined;
  return Boolean(caps?.torch);
}
