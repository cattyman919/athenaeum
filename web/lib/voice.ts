// Push-to-talk voice input — pure logic (docs/15 §2, §6, §8). The browser wiring lives in
// hooks/use-voice-input.ts; everything here is side-effect free and unit-tested.

import { ApiError } from "./api-client";

/** Client-enforced clip cap (docs/15 §6; the server backstops at 62 s). */
export const MAX_CLIP_MS = 60_000;
/** Upload cap for the `audio` part (10 MiB, docs/15 §6). */
export const MAX_AUDIO_BYTES = 10 * 1024 * 1024;

/** Containers tried in order; webm/opus is the contract default (docs/15 §3). */
export const PREFERRED_MIME_TYPES = [
  "audio/webm;codecs=opus",
  "audio/webm",
  "audio/ogg;codecs=opus",
  "audio/mp4",
] as const;

export type VoiceStatus = "idle" | "requesting" | "recording" | "transcribing";

/** Failure categories the composer renders (keys under `voice.errors.*`). */
export type VoiceErrorKind =
  | "denied"
  | "noDevice"
  | "unsupported"
  | "tooLarge"
  | "invalid"
  | "rateLimited"
  | "unavailable"
  | "network"
  | "generic";

export interface VoiceError {
  kind: VoiceErrorKind;
  /** Seconds until the transcribe bucket frees up (from Retry-After), for `rateLimited`. */
  retryAfter?: number;
}

export interface VoiceEnv {
  MediaRecorder?: unknown;
  getUserMedia?: unknown;
  isSecureContext?: boolean;
}

/** The mic is shown only where MediaRecorder + getUserMedia exist (docs/15 §2). */
export function isVoiceSupported(env: VoiceEnv): boolean {
  return (
    typeof env.MediaRecorder === "function" &&
    typeof env.getUserMedia === "function" &&
    env.isSecureContext !== false
  );
}

/** First container the recorder supports; "" lets the browser choose. */
export function pickMimeType(isTypeSupported?: (type: string) => boolean): string {
  if (!isTypeSupported) return "";
  for (const type of PREFERRED_MIME_TYPES) {
    try {
      if (isTypeSupported(type)) return type;
    } catch {
      // Some engines throw on unknown codec strings — treat as unsupported.
    }
  }
  return "";
}

/** File name whose extension matches the recorded container (the server sniffs via ffmpeg). */
export function clipFileName(mimeType: string): string {
  const base = mimeType.split(";")[0].trim().toLowerCase();
  if (base === "audio/ogg") return "clip.ogg";
  if (base === "audio/mp4") return "clip.m4a";
  return "clip.webm";
}

/** Maps a getUserMedia / MediaRecorder rejection to a composer state. */
export function classifyMediaError(err: unknown): VoiceError {
  const name = err && typeof err === "object" && "name" in err ? String(err.name) : "";
  switch (name) {
    case "NotAllowedError":
    case "PermissionDeniedError":
    case "SecurityError":
      return { kind: "denied" };
    case "NotFoundError":
    case "DevicesNotFoundError":
    case "OverconstrainedError":
    case "NotReadableError":
    case "TrackStartError":
      return { kind: "noDevice" };
    case "NotSupportedError":
    case "TypeError":
      return { kind: "unsupported" };
    default:
      return { kind: "generic" };
  }
}

/** Parses Retry-After (delta-seconds or HTTP-date) into whole seconds ≥ 1. */
export function parseRetryAfter(
  value: string | null | undefined,
  now = Date.now(),
): number | undefined {
  if (!value) return undefined;
  const trimmed = value.trim();
  if (/^\d+$/.test(trimmed)) return Math.max(1, Number.parseInt(trimmed, 10));
  const at = Date.parse(trimmed);
  if (Number.isNaN(at)) return undefined;
  return Math.max(1, Math.ceil((at - now) / 1000));
}

/** Maps a failed POST /api/chat/transcribe to a composer state (docs/15 §6). */
export function classifyTranscribeError(err: unknown): VoiceError {
  if (!(err instanceof ApiError)) return { kind: "network" };
  if (err.status === 429 || err.code === "RATE_LIMITED") {
    return { kind: "rateLimited", retryAfter: parseRetryAfter(err.headers?.get("Retry-After")) };
  }
  if (err.status === 503 || err.code === "TRANSCRIPTION_UNAVAILABLE")
    return { kind: "unavailable" };
  if (err.status === 400 || err.code === "VALIDATION_FAILED") return { kind: "invalid" };
  return { kind: "generic" };
}

/** Inserts a transcript beside any typed text; voice never replaces or submits (docs/15 §2). */
export function mergeTranscript(draft: string, transcript: string): string {
  const text = transcript.trim();
  if (!text) return draft;
  if (!draft.trim()) return text;
  return /\s$/.test(draft) ? `${draft}${text}` : `${draft} ${text}`;
}

/** m:ss for the recording indicator. */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

/** Cadence of live (interim) partial uploads while recording (docs/15 §2a). */
export const PARTIAL_INTERVAL_MS = 2_000;
/** MediaRecorder timeslice so chunks accumulate while recording (docs/15 §2a). */
export const RECORDER_TIMESLICE_MS = 1_000;

type IntervalHandle = ReturnType<typeof setInterval>;

export interface PartialSchedulerOptions {
  /** Cumulative clip recorded so far (all chunks since start), or null when empty. */
  snapshot: () => Blob | null;
  /** POST /api/chat/transcribe?partial=true; resolves to the interim text. */
  send: (audio: Blob) => Promise<string>;
  /** Receives each fresh interim text (trimmed); never called after stop(). */
  onText: (text: string) => void;
  intervalMs?: number;
  /** Injectable timers for tests. */
  setInterval?: (fn: () => void, ms: number) => IntervalHandle;
  clearInterval?: (id: IntervalHandle) => void;
}

/**
 * Live transcription by chunked re-transcription (docs/15 §2a). Every `intervalMs` while
 * running, and only when no partial is in flight, uploads the CUMULATIVE clip (later
 * MediaRecorder chunks are not independently decodable). Responses that are older than
 * the newest applied one, or that arrive after stop(), are dropped. Failures (429/503/
 * network) are swallowed: recording continues and the last interim text stays.
 * One instance per recording.
 */
export class PartialScheduler {
  private seq = 0;
  private applied = 0;
  private inFlight = false;
  private stopped = false;
  private timer: IntervalHandle | null = null;
  private lastSize = 0;

  constructor(private readonly opts: PartialSchedulerOptions) {}

  start(): void {
    if (this.timer !== null || this.stopped) return;
    const set = this.opts.setInterval ?? ((fn, ms) => setInterval(fn, ms));
    this.timer = set(() => void this.tick(), this.opts.intervalMs ?? PARTIAL_INTERVAL_MS);
  }

  /** Stops polling; in-flight responses are ignored from now on. */
  stop(): void {
    this.stopped = true;
    if (this.timer !== null) {
      (this.opts.clearInterval ?? ((id) => clearInterval(id)))(this.timer);
      this.timer = null;
    }
  }

  get busy(): boolean {
    return this.inFlight;
  }

  /** One polling step; exposed for tests. Resolves when the upload (if any) settles. */
  async tick(): Promise<void> {
    if (this.stopped || this.inFlight) return;
    const blob = this.opts.snapshot();
    // Nothing new since the last upload (or over the cap): skip this beat.
    if (!blob || blob.size === 0 || blob.size === this.lastSize || blob.size > MAX_AUDIO_BYTES)
      return;
    const id = ++this.seq;
    this.lastSize = blob.size;
    this.inFlight = true;
    try {
      const text = await this.opts.send(blob);
      if (this.stopped || id <= this.applied) return;
      this.applied = id;
      this.opts.onText(text.trim());
    } catch {
      // Silent by design (docs/15 §2a): no toast spam; the next beat retries.
    } finally {
      this.inFlight = false;
    }
  }
}

/** localStorage key for the chosen microphone (the id is only a hint; it may go stale). */
export const MIC_STORAGE_KEY = "athenaeum_mic";
/** Companion key holding the device label, used to re-find the mic if its id rotates. */
export const MIC_LABEL_STORAGE_KEY = "athenaeum_mic_label";

/** Resolves the saved choice against the current list: by id first, then by label. */
export function resolveMic(
  options: ReadonlyArray<MicOption>,
  id: string,
  label: string,
): MicOption | null {
  if (!id && !label) return null;
  return (
    options.find((o) => o.deviceId === id) ??
    (label ? options.find((o) => o.label === label) : undefined) ??
    null
  );
}

export interface MicOption {
  deviceId: string;
  label: string;
}

/** getUserMedia audio constraint: an exact device when one is chosen, else the default. */
export function audioConstraints(
  deviceId: string | null | undefined,
): MediaTrackConstraints | true {
  return deviceId ? { deviceId: { exact: deviceId } } : true;
}

/** True when the failure means the saved device is gone (retry with the default input). */
export function isStaleDeviceError(e: unknown): boolean {
  const name = (e as { name?: string } | null)?.name;
  return name === "OverconstrainedError" || name === "NotFoundError";
}

/** Audio inputs for the picker; the virtual "default"/"communications" entries are dropped
 *  (the "System default" option covers them) and unlabeled devices get a numbered name. */
export function toMicOptions(
  devices: ReadonlyArray<MediaDeviceInfo>,
  unnamed: (n: number) => string,
): MicOption[] {
  const out: MicOption[] = [];
  for (const d of devices) {
    if (d.kind !== "audioinput" || !d.deviceId) continue;
    if (d.deviceId === "default" || d.deviceId === "communications") continue;
    out.push({ deviceId: d.deviceId, label: d.label.trim() || unnamed(out.length + 1) });
  }
  return out;
}

/** Peak amplitude (0..1) of a time-domain byte buffer (128 = silence). */
export function peakLevel(samples: ArrayLike<number>): number {
  let max = 0;
  for (let i = 0; i < samples.length; i++) max = Math.max(max, Math.abs(samples[i] - 128));
  return Math.min(1, max / 128);
}
