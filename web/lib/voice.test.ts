import { describe, expect, it, vi } from "vitest";
import { ApiError, createApiClient } from "./api-client";
import {
  audioConstraints,
  isStaleDeviceError,
  peakLevel,
  toMicOptions,
  classifyMediaError,
  classifyTranscribeError,
  clipFileName,
  formatElapsed,
  isVoiceSupported,
  mergeTranscript,
  parseRetryAfter,
  pickMimeType,
} from "./voice";

const fn = () => undefined;
const named = (name: string) => Object.assign(new Error(name), { name });

describe("voice support detection (docs/15 §2)", () => {
  it("requires MediaRecorder and getUserMedia", () => {
    expect(isVoiceSupported({ MediaRecorder: fn, getUserMedia: fn, isSecureContext: true })).toBe(
      true,
    );
    expect(isVoiceSupported({ getUserMedia: fn })).toBe(false);
    expect(isVoiceSupported({ MediaRecorder: fn })).toBe(false);
    expect(isVoiceSupported({ MediaRecorder: fn, getUserMedia: fn, isSecureContext: false })).toBe(
      false,
    );
  });
});

describe("recorder container", () => {
  it("prefers webm/opus, falls back in order, or lets the browser choose", () => {
    expect(pickMimeType(() => true)).toBe("audio/webm;codecs=opus");
    expect(pickMimeType((t) => t === "audio/mp4")).toBe("audio/mp4");
    expect(pickMimeType(() => false)).toBe("");
    expect(pickMimeType(undefined)).toBe("");
    expect(
      pickMimeType((t) => {
        if (t.includes("opus")) throw new Error("bad codec");
        return t === "audio/webm";
      }),
    ).toBe("audio/webm");
  });

  it("names the clip after its container", () => {
    expect(clipFileName("audio/webm;codecs=opus")).toBe("clip.webm");
    expect(clipFileName("audio/ogg;codecs=opus")).toBe("clip.ogg");
    expect(clipFileName("audio/mp4")).toBe("clip.m4a");
    expect(clipFileName("")).toBe("clip.webm");
  });
});

describe("media errors", () => {
  it("maps permission, device and support failures", () => {
    expect(classifyMediaError(named("NotAllowedError"))).toEqual({ kind: "denied" });
    expect(classifyMediaError(named("SecurityError"))).toEqual({ kind: "denied" });
    expect(classifyMediaError(named("NotFoundError"))).toEqual({ kind: "noDevice" });
    expect(classifyMediaError(named("NotReadableError"))).toEqual({ kind: "noDevice" });
    expect(classifyMediaError(named("NotSupportedError"))).toEqual({ kind: "unsupported" });
    expect(classifyMediaError("boom")).toEqual({ kind: "generic" });
  });
});

describe("transcribe errors (docs/15 §6)", () => {
  const err = (status: number, code: string, headers?: Record<string, string>) =>
    new ApiError(status, code, "x", undefined, new Headers(headers));

  it("maps 429 with Retry-After", () => {
    expect(classifyTranscribeError(err(429, "RATE_LIMITED", { "Retry-After": "12" }))).toEqual({
      kind: "rateLimited",
      retryAfter: 12,
    });
    expect(classifyTranscribeError(err(429, "RATE_LIMITED"))).toEqual({
      kind: "rateLimited",
      retryAfter: undefined,
    });
  });

  it("maps 503, 400 and the rest", () => {
    expect(classifyTranscribeError(err(503, "TRANSCRIPTION_UNAVAILABLE"))).toEqual({
      kind: "unavailable",
    });
    expect(classifyTranscribeError(err(400, "VALIDATION_FAILED"))).toEqual({ kind: "invalid" });
    expect(classifyTranscribeError(err(500, "INTERNAL"))).toEqual({ kind: "generic" });
    expect(classifyTranscribeError(new TypeError("Failed to fetch"))).toEqual({ kind: "network" });
  });

  it("parses Retry-After seconds and HTTP dates", () => {
    expect(parseRetryAfter("30")).toBe(30);
    expect(parseRetryAfter("0")).toBe(1);
    expect(parseRetryAfter(null)).toBeUndefined();
    expect(parseRetryAfter("soon")).toBeUndefined();
    const now = Date.parse("2026-10-10T12:00:00Z");
    expect(parseRetryAfter("Sat, 10 Oct 2026 12:00:45 GMT", now)).toBe(45);
  });
});

describe("transcript insertion (docs/15 §2)", () => {
  it("inserts beside typed text without replacing it", () => {
    expect(mergeTranscript("", " where is Neuromancer ")).toBe("where is Neuromancer");
    expect(mergeTranscript("Hi,", "where is it")).toBe("Hi, where is it");
    expect(mergeTranscript("Hi, ", "where is it")).toBe("Hi, where is it");
    expect(mergeTranscript("keep me", "   ")).toBe("keep me");
    expect(mergeTranscript("   ", "text")).toBe("text");
  });

  it("formats the recording timer", () => {
    expect(formatElapsed(0)).toBe("0:00");
    expect(formatElapsed(3_400)).toBe("0:03");
    expect(formatElapsed(60_000)).toBe("1:00");
    expect(formatElapsed(-5)).toBe("0:00");
  });
});

describe("api.transcribe (docs/03 §7.2)", () => {
  it("posts multipart audio + locale without forcing a JSON content type", async () => {
    let seen: RequestInit | undefined;
    const fetcher = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      seen = init;
      return new Response(JSON.stringify({ text: "hello", language: "en" }), { status: 200 });
    });
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    const res = await c.transcribe(new Blob(["abc"], { type: "audio/webm" }), "zh-CN");
    expect(res).toEqual({ text: "hello", language: "en" });
    expect(fetcher.mock.calls[0][0]).toBe("/api/chat/transcribe");
    expect(seen?.method).toBe("POST");
    expect(new Headers(seen?.headers).has("Content-Type")).toBe(false);
    const form = seen?.body as FormData;
    expect(form.get("locale")).toBe("zh-CN");
    const audio = form.get("audio") as File;
    expect(audio.name).toBe("clip.webm");
    expect(audio.size).toBe(3);
  });

  it("surfaces the error envelope with Retry-After", async () => {
    const fetcher = vi.fn(
      async () =>
        new Response(JSON.stringify({ error: "RATE_LIMITED", message: "slow down" }), {
          status: 429,
          headers: { "Retry-After": "7" },
        }),
    );
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    const e = await c.transcribe(new Blob(["a"]), "en").catch((x: unknown) => x);
    expect(classifyTranscribeError(e)).toEqual({ kind: "rateLimited", retryAfter: 7 });
  });
});

describe("microphone selection", () => {
  const dev = (kind: string, deviceId: string, label: string) =>
    ({ kind, deviceId, label }) as unknown as MediaDeviceInfo;

  it("uses an exact constraint only when a device is chosen", () => {
    expect(audioConstraints("")).toBe(true);
    expect(audioConstraints("abc")).toEqual({ deviceId: { exact: "abc" } });
  });

  it("lists real audio inputs, hides virtual entries and names unlabeled ones", () => {
    const opts = toMicOptions(
      [
        dev("audioinput", "default", "Default - Built-in"),
        dev("audioinput", "communications", "Comms"),
        dev("audioinput", "a1", "USB Mic"),
        dev("audioinput", "a2", " "),
        dev("videoinput", "v1", "Camera"),
        dev("audiooutput", "o1", "Speakers"),
      ],
      (n) => `Microphone ${n}`,
    );
    expect(opts).toEqual([
      { deviceId: "a1", label: "USB Mic" },
      { deviceId: "a2", label: "Microphone 2" },
    ]);
  });

  it("recognises a stale saved device and measures peak level", () => {
    expect(isStaleDeviceError({ name: "OverconstrainedError" })).toBe(true);
    expect(isStaleDeviceError({ name: "NotAllowedError" })).toBe(false);
    expect(peakLevel(new Uint8Array([128, 128, 128]))).toBe(0);
    expect(peakLevel(new Uint8Array([128, 192, 64]))).toBeCloseTo(0.5);
  });
});
