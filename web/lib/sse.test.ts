import { describe, expect, it } from "vitest";
import { createSSEParser } from "./sse";

describe("SSE parser (docs/07 §6)", () => {
  it("parses named events and ignores ping comments", () => {
    const p = createSSEParser();
    const frames = p.push(
      'event: meta\ndata: {"turn":1}\n\n: ping\n\nevent: token\ndata: {"text":"Hi"}\n\n',
    );
    expect(frames).toEqual([
      { event: "meta", data: '{"turn":1}' },
      { event: "token", data: '{"text":"Hi"}' },
    ]);
  });

  it("buffers frames split across chunks, including multi-byte text", () => {
    const p = createSSEParser();
    expect(p.push("event: tok")).toEqual([]);
    expect(p.push('en\ndata: {"text":"我在')).toEqual([]);
    expect(p.push('"}\n')).toEqual([]);
    expect(p.push("\n")).toEqual([{ event: "token", data: '{"text":"我在"}' }]);
  });

  it("normalizes CRLF even when split at the chunk edge", () => {
    const p = createSSEParser();
    expect(p.push("event: done\r\ndata: {}\r")).toEqual([]);
    expect(p.push("\n\r\n")).toEqual([{ event: "done", data: "{}" }]);
  });

  it("defaults the event name and joins multi-line data", () => {
    const p = createSSEParser();
    expect(p.push("data: a\ndata: b\n\n")).toEqual([{ event: "message", data: "a\nb" }]);
  });

  it("flushes an unterminated trailing frame on end()", () => {
    const p = createSSEParser();
    expect(p.push('event: done\ndata: {"x":1}')).toEqual([]);
    expect(p.end()).toEqual([{ event: "done", data: '{"x":1}' }]);
  });
});
