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
