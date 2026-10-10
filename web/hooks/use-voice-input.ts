"use client";

// Push-to-talk capture for the chat composer (docs/15 §2, §8): MediaRecorder (webm/opus),
// permission handling, record/stop state, POST /api/chat/transcribe with the active locale
// as the Whisper hint, and transcript hand-off. Voice never auto-submits.

import * as React from "react";
import { api } from "@/lib/api-client";
import type { Locale } from "@/lib/locale";
import type { TranscribeResponse } from "@/lib/types";
import {
  MAX_AUDIO_BYTES,
  MAX_CLIP_MS,
  classifyMediaError,
  classifyTranscribeError,
  clipFileName,
  isVoiceSupported,
  pickMimeType,
  type VoiceError,
  type VoiceStatus,
} from "@/lib/voice";

export interface UseVoiceInputOptions {
  locale: Locale;
  /** Receives the trimmed transcript; called with "" when nothing was recognised. */
  onTranscript: (text: string) => void;
  onError?: (error: VoiceError) => void;
  /** Injectable for tests. */
  transcribe?: (audio: Blob, locale: Locale, fileName: string) => Promise<TranscribeResponse>;
}

const noopSubscribe = () => () => {};

function browserSupportsVoice(): boolean {
  return isVoiceSupported({
    MediaRecorder: typeof MediaRecorder === "undefined" ? undefined : MediaRecorder,
    getUserMedia: navigator.mediaDevices?.getUserMedia,
    isSecureContext: window.isSecureContext,
  });
}

export function useVoiceInput(opts: UseVoiceInputOptions) {
  // Server render and first hydration pass report "unsupported", so the mic only appears
  // client-side where the APIs actually exist (no hydration mismatch).
  const supported = React.useSyncExternalStore(noopSubscribe, browserSupportsVoice, () => false);
  const [status, setStatus] = React.useState<VoiceStatus>("idle");
  const [error, setError] = React.useState<VoiceError | null>(null);
  const [startedAt, setStartedAt] = React.useState<number | null>(null);
  const [now, setNow] = React.useState(0);

  const optsRef = React.useRef(opts);
  React.useEffect(() => {
    optsRef.current = opts;
  });
  const recorderRef = React.useRef<MediaRecorder | null>(null);
  const streamRef = React.useRef<MediaStream | null>(null);
  const chunksRef = React.useRef<Blob[]>([]);
  const capTimer = React.useRef<number | null>(null);
  const discardRef = React.useRef(false);
  const mountedRef = React.useRef(true);

  const fail = React.useCallback((e: VoiceError) => {
    if (!mountedRef.current) return;
    setError(e);
    setStatus("idle");
    optsRef.current.onError?.(e);
  }, []);

  const releaseStream = React.useCallback(() => {
    if (capTimer.current !== null) {
      window.clearTimeout(capTimer.current);
      capTimer.current = null;
    }
    streamRef.current?.getTracks().forEach((t) => t.stop());
    streamRef.current = null;
  }, []);

  const upload = React.useCallback(
    async (blob: Blob, mimeType: string) => {
      if (blob.size === 0) {
        if (mountedRef.current) setStatus("idle");
        optsRef.current.onTranscript("");
        return;
      }
      if (blob.size > MAX_AUDIO_BYTES) {
        fail({ kind: "tooLarge" });
        return;
      }
      if (mountedRef.current) setStatus("transcribing");
      const { locale, transcribe } = optsRef.current;
      try {
        const res = await (transcribe ?? api.transcribe)(blob, locale, clipFileName(mimeType));
        if (!mountedRef.current) return;
        setStatus("idle");
        optsRef.current.onTranscript((res.text ?? "").trim());
      } catch (e) {
        fail(classifyTranscribeError(e));
      }
    },
    [fail],
  );

  const stop = React.useCallback(() => {
    const rec = recorderRef.current;
    if (rec && rec.state !== "inactive") rec.stop();
  }, []);

  const start = React.useCallback(async () => {
    if (recorderRef.current || status !== "idle") return;
    setError(null);
    if (!browserSupportsVoice()) {
      fail({ kind: "unsupported" });
      return;
    }
    setStatus("requesting");
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (e) {
      fail(classifyMediaError(e));
      return;
    }
    if (!mountedRef.current) {
      stream.getTracks().forEach((t) => t.stop());
      return;
    }
    streamRef.current = stream;

    let rec: MediaRecorder;
    try {
      const mimeType = pickMimeType((t) => MediaRecorder.isTypeSupported(t));
      rec = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    } catch (e) {
      releaseStream();
      fail(classifyMediaError(e));
      return;
    }
    recorderRef.current = rec;
    chunksRef.current = [];
    discardRef.current = false;
    rec.ondataavailable = (ev) => {
      if (ev.data && ev.data.size > 0) chunksRef.current.push(ev.data);
    };
    rec.onstop = () => {
      const type = rec.mimeType || "audio/webm";
      const blob = new Blob(chunksRef.current, { type });
      chunksRef.current = [];
      recorderRef.current = null;
      releaseStream();
      if (mountedRef.current) setStartedAt(null);
      if (discardRef.current || !mountedRef.current) {
        if (mountedRef.current) setStatus("idle");
        return;
      }
      void upload(blob, type);
    };
    rec.onerror = () => {
      discardRef.current = true;
      fail({ kind: "generic" });
      if (rec.state !== "inactive") rec.stop();
    };

    rec.start();
    const t0 = Date.now();
    setStartedAt(t0);
    setNow(t0);
    setStatus("recording");
    // Client-enforced 60 s cap (docs/15 §6): stop and transcribe what we have.
    capTimer.current = window.setTimeout(() => stop(), MAX_CLIP_MS);
  }, [status, fail, releaseStream, upload, stop]);

  /** Abandons an in-progress recording without uploading it. */
  const cancel = React.useCallback(() => {
    discardRef.current = true;
    stop();
  }, [stop]);

  const toggle = React.useCallback(() => {
    if (status === "recording") stop();
    else if (status === "idle") void start();
  }, [status, start, stop]);

  // Tick the recording indicator.
  React.useEffect(() => {
    if (status !== "recording") return;
    const id = window.setInterval(() => setNow(Date.now()), 250);
    return () => window.clearInterval(id);
  }, [status]);

  // Release the microphone on unmount; never upload after leaving the page.
  React.useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      discardRef.current = true;
      const rec = recorderRef.current;
      if (rec && rec.state !== "inactive") rec.stop();
      releaseStream();
    };
  }, [releaseStream]);

  return {
    supported,
    status,
    error,
    recording: status === "recording",
    busy: status !== "idle",
    elapsedMs: startedAt === null ? 0 : Math.max(0, now - startedAt),
    start,
    stop,
    cancel,
    toggle,
    clearError: () => setError(null),
  };
}
