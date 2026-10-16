import { describe, expect, it, vi } from "vitest";
import {
  SCAN_COOLDOWN_MS,
  cameraConstraints,
  createScanner,
  type Detector,
  type ScannerCallbacks,
  type StreamLike,
  type VideoLike,
} from "./barcode-scanner";
import { classifyScanCode, type ScanVerdict } from "./barcode";

/** Manual frame pump + fake clock + fake camera/detector (no DOM needed). */
function harness(
  opts: {
    reads?: Array<string[] | Error>;
    gum?: (c: MediaStreamConstraints) => Promise<StreamLike>;
    noGum?: boolean;
    detectorFails?: boolean;
    torch?: boolean;
    classify?: (raw: string) => ScanVerdict;
    format?: string;
  } = {},
) {
  let clock = 0;
  const frames = new Map<number, () => void>();
  let nextId = 1;
  const reads = [...(opts.reads ?? [])];
  const stop = vi.fn();
  const applyConstraints = vi.fn(async () => undefined);
  const track = {
    stop,
    applyConstraints,
    getCapabilities: () => (opts.torch ? { torch: true } : {}),
  };
  const stream: StreamLike = { getTracks: () => [track], getVideoTracks: () => [track] };
  const gum = vi.fn(opts.gum ?? (async () => stream));
  const detector: Detector = {
    detect: vi.fn(async () => {
      const r = reads.shift() ?? [];
      if (r instanceof Error) throw r;
      return r.map((rawValue) => ({ rawValue, format: opts.format ?? "ean_13" }));
    }),
  };
  const createDetector = vi.fn(async () => {
    if (opts.detectorFails) throw new Error("wasm fetch failed");
    return detector;
  });
  const cb = {
    onDetected: vi.fn(),
    onRejected: vi.fn(),
    onError: vi.fn(),
    onStarted: vi.fn(),
  } satisfies ScannerCallbacks;
  const scanner = createScanner(
    {
      getUserMedia: opts.noGum ? undefined : gum,
      createDetector,
      requestFrame: (f) => {
        const id = nextId++;
        frames.set(id, f);
        return id;
      },
      cancelFrame: (id) => frames.delete(id),
      now: () => clock,
      classify: opts.classify,
    },
    cb,
  );
  const video: VideoLike = { srcObject: null, currentTime: 0, play: vi.fn(async () => undefined) };

  /** Advances the video by one frame and runs the pending frame callback. */
  async function frame(dtMs = 100) {
    clock += dtMs;
    video.currentTime += dtMs / 1000;
    const pending = [...frames];
    frames.clear();
    for (const [, f] of pending) f();
    await new Promise((r) => setTimeout(r, 0));
  }

  return { scanner, video, cb, gum, stop, frames, frame, detector, applyConstraints, stream };
}

describe("createScanner — camera path (docs/11 §5/§7/§8)", () => {
  it("requests the rear camera with the documented constraints", async () => {
    const h = harness();
    await h.scanner.start(h.video);
    expect(h.gum).toHaveBeenCalledWith(cameraConstraints("environment"));
    expect(cameraConstraints("user").video).toEqual({
      facingMode: { ideal: "user" },
      width: { ideal: 1920 },
    });
    expect(h.video.srcObject).toBe(h.stream);
    expect(h.cb.onStarted).toHaveBeenCalledWith({ torch: false });
    expect(h.scanner.running).toBe(true);
  });

  it("emits a valid ISBN and enforces the 1.5 s cooldown", async () => {
    const h = harness({ reads: [["9780553293357"], ["9780553293357"], [], ["9780441013593"]] });
    await h.scanner.start(h.video);
    await h.frame(); // detect #1 → emit
    await h.frame(); // detect #2 → within cooldown, ignored
    await h.frame(); // no hit
    expect(h.cb.onDetected).toHaveBeenCalledTimes(1);
    expect(h.cb.onDetected).toHaveBeenCalledWith("9780553293357", "ean_13");
    await h.frame(SCAN_COOLDOWN_MS); // cooldown elapsed → next code emits
    expect(h.cb.onDetected).toHaveBeenLastCalledWith("9780441013593", "ean_13");
    expect(h.cb.onDetected).toHaveBeenCalledTimes(2);
  });

  it("ignores copy tags with the default ISBN-only classifier (admin intake)", async () => {
    const h = harness({ reads: [["ATH-002941-C01"]], format: "code_128" });
    await h.scanner.start(h.video);
    await h.frame();
    expect(h.cb.onDetected).not.toHaveBeenCalled();
    expect(h.cb.onRejected.mock.calls[0][0].kind).toBe("invalid");
  });

  it("emits Code 128 copy tags with the universal classifier (docs/11 §3)", async () => {
    const h = harness({
      reads: [["ath-002941-c01"], ["0441013597"]],
      format: "code_128",
      classify: classifyScanCode,
    });
    await h.scanner.start(h.video);
    await h.frame();
    expect(h.cb.onDetected).toHaveBeenCalledWith("ATH-002941-C01", "code_128");
    await h.frame(SCAN_COOLDOWN_MS);
    expect(h.cb.onRejected.mock.calls.map((c) => c[0].kind)).toEqual(["isbn10"]);
  });

  it("skips detect() when the video frame has not advanced", async () => {
    const h = harness({ reads: [[]] });
    await h.scanner.start(h.video);
    await h.frame();
    await h.frame(0); // same currentTime
    expect(h.detector.detect).toHaveBeenCalledTimes(1);
  });

  it("rejects checksum failures and UPC store barcodes", async () => {
    const h = harness({ reads: [["9780553293358"], ["036000291452"]] });
    await h.scanner.start(h.video);
    await h.frame();
    await h.frame(SCAN_COOLDOWN_MS);
    expect(h.cb.onDetected).not.toHaveBeenCalled();
    expect(h.cb.onRejected.mock.calls.map((c) => c[0].kind)).toEqual(["invalid", "upc"]);
  });

  it("stop() releases every track and cancels the frame loop", async () => {
    const h = harness();
    await h.scanner.start(h.video);
    expect(h.frames.size).toBe(1);
    h.scanner.stop();
    expect(h.stop).toHaveBeenCalled();
    expect(h.frames.size).toBe(0);
    expect(h.scanner.running).toBe(false);
  });

  it("is StrictMode-safe: a stop during the permission prompt discards the stream", async () => {
    let resolve!: (s: StreamLike) => void;
    const h = harness({ gum: () => new Promise((r) => (resolve = r)) });
    const started = h.scanner.start(h.video);
    await new Promise((r) => setTimeout(r, 0));
    h.scanner.stop();
    resolve(h.stream);
    await started;
    expect(h.stop).toHaveBeenCalled();
    expect(h.video.srcObject).toBeNull();
    expect(h.frames.size).toBe(0);
    expect(h.cb.onStarted).not.toHaveBeenCalled();
  });

  it.each([
    ["NotAllowedError", "permission"],
    ["NotFoundError", "no_device"],
  ] as const)("getUserMedia %s → %s", async (name, kind) => {
    const h = harness({
      gum: async () => {
        throw Object.assign(new Error(name), { name });
      },
    });
    await h.scanner.start(h.video);
    expect(h.cb.onError).toHaveBeenCalledWith(kind, expect.anything());
    expect(h.scanner.running).toBe(false);
  });

  it("reports an insecure origin when getUserMedia is absent", async () => {
    const h = harness({ noGum: true });
    await h.scanner.start(h.video);
    expect(h.cb.onError).toHaveBeenCalledWith("insecure", expect.anything());
  });

  it("reports WASM load failures from the detector factory and from detect()", async () => {
    const a = harness({ detectorFails: true });
    await a.scanner.start(a.video);
    expect(a.cb.onError).toHaveBeenCalledWith("wasm", expect.anything());
    expect(a.gum).not.toHaveBeenCalled();

    const b = harness({ reads: [new Error("zxing wasm 404")] });
    await b.scanner.start(b.video);
    await b.frame();
    expect(b.cb.onError).toHaveBeenCalledWith("wasm", expect.anything());
    expect(b.stop).toHaveBeenCalled();
    expect(b.frames.size).toBe(0);
  });

  it("toggles the torch only on capable tracks", async () => {
    const plain = harness();
    await plain.scanner.start(plain.video);
    expect(await plain.scanner.setTorch(true)).toBe(false);

    const lit = harness({ torch: true });
    await lit.scanner.start(lit.video);
    expect(lit.cb.onStarted).toHaveBeenCalledWith({ torch: true });
    expect(await lit.scanner.setTorch(true)).toBe(true);
    expect(lit.applyConstraints).toHaveBeenCalledWith({ advanced: [{ torch: true }] });
  });

  it("restarts with the front camera for flips", async () => {
    const h = harness();
    await h.scanner.start(h.video);
    await h.scanner.start(h.video, "user");
    expect(h.stop).toHaveBeenCalledTimes(1); // first stream released
    expect(h.gum).toHaveBeenLastCalledWith(cameraConstraints("user"));
    expect(h.frames.size).toBe(1);
  });
});
