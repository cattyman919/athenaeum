"use client";

// Push-to-talk capture for the chat composer (docs/15 §2, §8): MediaRecorder (webm/opus),
// permission handling, record/stop state, POST /api/chat/transcribe with the active locale
// as the Whisper hint, and transcript hand-off. Voice never auto-submits. While recording,
// a PartialScheduler re-transcribes the cumulative clip every ~2 s (?partial=true) and
// exposes the result as `interim` text (docs/15 §2a); the stop-time final pass replaces it.

import * as React from "react";
import { api } from "@/lib/api-client";
import type { Locale } from "@/lib/locale";
import type { TranscribeResponse } from "@/lib/types";
import {
  MIC_LABEL_STORAGE_KEY,
  MIC_STORAGE_KEY,
  resolveMic,
  audioConstraints,
  isStaleDeviceError,
  peakLevel,
  toMicOptions,
  type MicOption,
  MAX_AUDIO_BYTES,
  MAX_CLIP_MS,
  PartialScheduler,
  RECORDER_TIMESLICE_MS,
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
  /** Label for unnamed devices, e.g. "Microphone 2". */
  unnamedMic?: (n: number) => string;
  /** Injectable for tests. */
  transcribe?: (
    audio: Blob,
    locale: Locale,
    fileName: string,
    opts?: { partial?: boolean },
  ) => Promise<TranscribeResponse>;
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
  /** Live interim transcript while recording/transcribing (docs/15 §2a); "" otherwise. */
  const [interim, setInterim] = React.useState("");
  /** Available audio inputs, the saved choice ("" = system default) and the live input level. */
  const [mics, setMics] = React.useState<MicOption[]>([]);
  const [deviceId, setDeviceIdState] = React.useState("");
  const [level, setLevel] = React.useState(0);
  const levelCleanup = React.useRef<(() => void) | null>(null);

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
  const partialsRef = React.useRef<PartialScheduler | null>(null);

  const stopPartials = React.useCallback(() => {
    partialsRef.current?.stop();
    partialsRef.current = null;
  }, []);

  const fail = React.useCallback((e: VoiceError) => {
    if (!mountedRef.current) return;
    setError(e);
    setStatus("idle");
    setInterim("");
    optsRef.current.onError?.(e);
  }, []);

  const releaseStream = React.useCallback(() => {
    if (capTimer.current !== null) {
      window.clearTimeout(capTimer.current);
      capTimer.current = null;
    }
    streamRef.current?.getTracks().forEach((t) => t.stop());
    streamRef.current = null;
    levelCleanup.current?.();
    levelCleanup.current = null;
  }, []);

  const upload = React.useCallback(
    async (blob: Blob, mimeType: string) => {
      if (blob.size === 0) {
        if (mountedRef.current) {
          setStatus("idle");
          setInterim("");
        }
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
        setInterim("");
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

  const refreshMics = React.useCallback(async () => {
    try {
      const all = await navigator.mediaDevices.enumerateDevices();
      if (mountedRef.current) {
        setMics(toMicOptions(all, optsRef.current.unnamedMic ?? ((n) => `Microphone ${n}`)));
      }
    } catch {
      /* device listing is best effort */
    }
  }, []);

  const setDeviceId = React.useCallback((id: string, label = "") => {
    setDeviceIdState(id);
    try {
      if (id) {
        window.localStorage.setItem(MIC_STORAGE_KEY, id);
        window.localStorage.setItem(MIC_LABEL_STORAGE_KEY, label);
      } else {
        window.localStorage.removeItem(MIC_STORAGE_KEY);
        window.localStorage.removeItem(MIC_LABEL_STORAGE_KEY);
      }
    } catch {
      /* storage may be unavailable */
    }
  }, []);

  // The device id can rotate (cleared site data); re-find the saved mic by label.
  React.useEffect(() => {
    if (!deviceId || mics.length === 0) return;
    let label = "";
    try {
      label = window.localStorage.getItem(MIC_LABEL_STORAGE_KEY) ?? "";
    } catch {
      /* ignore */
    }
    const found = resolveMic(mics, deviceId, label);
    if (found && found.deviceId !== deviceId) setDeviceId(found.deviceId, found.label);
  }, [mics, deviceId, setDeviceId]);

  // Restore the saved choice, list devices, and follow plug/unplug events.
  React.useEffect(() => {
    try {
      setDeviceIdState(window.localStorage.getItem(MIC_STORAGE_KEY) ?? "");
    } catch {
      /* ignore */
    }
    const md = navigator.mediaDevices;
    if (!md?.enumerateDevices) return;
    void refreshMics();
    md.addEventListener?.("devicechange", refreshMics);
    return () => md.removeEventListener?.("devicechange", refreshMics);
  }, [refreshMics]);

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
      try {
        stream = await navigator.mediaDevices.getUserMedia({ audio: audioConstraints(deviceId) });
      } catch (e) {
        if (!deviceId || !isStaleDeviceError(e)) throw e;
        // The saved microphone is gone: forget it and fall back to the system default.
        setDeviceId("");
        stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      }
    } catch (e) {
      fail(classifyMediaError(e));
      return;
    }
    void refreshMics(); // labels become available once permission is granted
    if (!mountedRef.current) {
      stream.getTracks().forEach((t) => t.stop());
      return;
    }
    streamRef.current = stream;
    levelCleanup.current?.();
    try {
      const ctx = new AudioContext();
      const analyser = ctx.createAnalyser();
      ctx.createMediaStreamSource(stream).connect(analyser);
      const buf = new Uint8Array(analyser.fftSize);
      let peak = 0;
      const id = window.setInterval(() => {
        analyser.getByteTimeDomainData(buf);
        peak = Math.max(peak * 0.7, peakLevel(buf));
        if (mountedRef.current) setLevel(peak);
      }, 100);
      levelCleanup.current = () => {
        window.clearInterval(id);
        void ctx.close();
        setLevel(0);
      };
    } catch {
      /* the meter is optional */
    }

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
      stopPartials();
      const type = rec.mimeType || "audio/webm";
      const blob = new Blob(chunksRef.current, { type });
      chunksRef.current = [];
      recorderRef.current = null;
      releaseStream();
      if (mountedRef.current) setStartedAt(null);
      if (discardRef.current || !mountedRef.current) {
        if (mountedRef.current) {
          setStatus("idle");
          setInterim("");
        }
        return;
      }
      void upload(blob, type);
    };
    rec.onerror = () => {
      discardRef.current = true;
      fail({ kind: "generic" });
      if (rec.state !== "inactive") rec.stop();
    };

    // Timeslice so chunks accumulate for the live partials (docs/15 §2a).
    rec.start(RECORDER_TIMESLICE_MS);
    setInterim("");
    const partialType = rec.mimeType || "audio/webm";
    const partials = new PartialScheduler({
      snapshot: () =>
        chunksRef.current.length ? new Blob(chunksRef.current, { type: partialType }) : null,
      send: async (audio) => {
        const { locale, transcribe } = optsRef.current;
        const res = await (transcribe ?? api.transcribe)(audio, locale, clipFileName(partialType), {
          partial: true,
        });
        return res.text ?? "";
      },
      onText: (text) => {
        if (mountedRef.current) setInterim(text);
      },
    });
    partialsRef.current = partials;
    partials.start();
    const t0 = Date.now();
    setStartedAt(t0);
    setNow(t0);
    setStatus("recording");
    // Client-enforced 60 s cap (docs/15 §6): stop and transcribe what we have.
    capTimer.current = window.setTimeout(() => stop(), MAX_CLIP_MS);
  }, [status, deviceId, fail, releaseStream, upload, stop, stopPartials, refreshMics, setDeviceId]);

  /** Abandons an in-progress recording without uploading it. */
  const cancel = React.useCallback(() => {
    discardRef.current = true;
    stopPartials();
    setInterim("");
    stop();
  }, [stop, stopPartials]);

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
      partialsRef.current?.stop();
      partialsRef.current = null;
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
    interim,
    mics,
    deviceId,
    setDeviceId,
    level,
    start,
    stop,
    cancel,
    toggle,
    clearError: () => setError(null),
  };
}
