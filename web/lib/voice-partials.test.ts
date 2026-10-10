import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api-client";
import { MAX_AUDIO_BYTES, PARTIAL_INTERVAL_MS, PartialScheduler } from "./voice";

// Live (interim) transcription polling — docs/15 §2a.

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/** A recorder whose cumulative clip grows by `step` bytes per call to grow(). */
function fakeClip(step = 100) {
  let size = 0;
  return {
    grow: () => {
      size += step;
    },
    snapshot: () => (size ? new Blob([new Uint8Array(size)], { type: "audio/webm" }) : null),
  };
}

const flush = () => new Promise((r) => setTimeout(r, 0));

describe("PartialScheduler", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("uploads the cumulative clip every interval and reports interim text", async () => {
    const clip = fakeClip();
    const sizes: number[] = [];
    const texts: string[] = [];
    const s = new PartialScheduler({
      snapshot: clip.snapshot,
      send: async (b) => {
        sizes.push(b.size);
        return ` partial ${b.size} `;
      },
      onText: (t) => texts.push(t),
    });
    s.start();
    clip.grow();
    vi.advanceTimersByTime(PARTIAL_INTERVAL_MS);
    await flush();
    clip.grow();
    vi.advanceTimersByTime(PARTIAL_INTERVAL_MS);
    await flush();
    s.stop();
    expect(sizes).toEqual([100, 200]); // whole blob so far, never just the newest chunk
    expect(texts).toEqual(["partial 100", "partial 200"]);
  });

  it("skips beats while a partial is in flight (one at a time)", async () => {
    const clip = fakeClip();
    const pending = deferred<string>();
    const send = vi.fn(() => pending.promise);
    const s = new PartialScheduler({ snapshot: clip.snapshot, send, onText: vi.fn() });
    s.start();
    for (let i = 0; i < 4; i++) {
      clip.grow();
      vi.advanceTimersByTime(PARTIAL_INTERVAL_MS);
    }
    expect(send).toHaveBeenCalledTimes(1);
    expect(s.busy).toBe(true);
    pending.resolve("x");
    await flush();
    clip.grow();
    vi.advanceTimersByTime(PARTIAL_INTERVAL_MS);
    expect(send).toHaveBeenCalledTimes(2);
    s.stop();
  });

  it("does nothing before audio exists, when nothing new was recorded, or over the cap", async () => {
    let blob: Blob | null = null;
    const send = vi.fn(async () => "x");
    const s = new PartialScheduler({ snapshot: () => blob, send, onText: vi.fn() });
    await s.tick(); // no chunks yet
    blob = new Blob([new Uint8Array(10)]);
    await s.tick();
    await s.tick(); // same size → skipped
    blob = { size: MAX_AUDIO_BYTES + 1 } as Blob;
    await s.tick();
    expect(send).toHaveBeenCalledTimes(1);
  });

  it("drops responses that arrive after stop (cancel/unmount/final pass)", async () => {
    const clip = fakeClip();
    const pending = deferred<string>();
    const onText = vi.fn();
    const s = new PartialScheduler({
      snapshot: clip.snapshot,
      send: () => pending.promise,
      onText,
    });
    s.start();
    clip.grow();
    const tick = s.tick();
    s.stop();
    pending.resolve("late");
    await tick;
    expect(onText).not.toHaveBeenCalled();
    // Stopped: the timer is cleared and further beats are no-ops.
    clip.grow();
    vi.advanceTimersByTime(PARTIAL_INTERVAL_MS * 3);
    await s.tick();
    expect(onText).not.toHaveBeenCalled();
  });

  it("ignores stale out-of-order responses", async () => {
    const clip = fakeClip();
    const first = deferred<string>();
    const second = deferred<string>();
    const queue = [first, second];
    const onText = vi.fn();
    const s = new PartialScheduler({
      snapshot: clip.snapshot,
      send: () => queue.shift()!.promise,
      onText,
    });
    clip.grow();
    const t1 = s.tick();
    // Force overlap (normally prevented by the in-flight guard) to prove the seq check.
    (s as unknown as { inFlight: boolean }).inFlight = false;
    clip.grow();
    const t2 = s.tick();
    second.resolve("newer");
    await t2;
    first.resolve("older");
    await t1;
    expect(onText.mock.calls).toEqual([["newer"]]);
  });

  it("swallows 429/503/network failures and keeps polling", async () => {
    const clip = fakeClip();
    const failures = [
      new ApiError(429, "RATE_LIMITED", "busy"),
      new ApiError(503, "TRANSCRIPTION_UNAVAILABLE", "down"),
      new TypeError("network"),
    ];
    const onText = vi.fn();
    const send = vi.fn(async () => {
      const f = failures.shift();
      if (f) throw f;
      return "recovered";
    });
    const s = new PartialScheduler({ snapshot: clip.snapshot, send, onText });
    for (let i = 0; i < 4; i++) {
      clip.grow();
      await expect(s.tick()).resolves.toBeUndefined();
    }
    expect(send).toHaveBeenCalledTimes(4);
    expect(onText.mock.calls).toEqual([["recovered"]]);
  });
});
