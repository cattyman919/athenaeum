import { describe, expect, it } from "vitest";
import {
  chatReducer,
  parseShelfPath,
  STREAM_INTERRUPTED,
  turnsFromHistory,
  type ChatAction,
  type ChatTurn,
} from "./chat-stream";

const run = (actions: ChatAction[], init: ChatTurn[] = []) => actions.reduce(chatReducer, init);
const ev = (event: string, data: unknown, at = 1): ChatAction => ({
  type: "event",
  key: "k",
  event,
  data,
  at,
});

describe("chat turn reducer (docs/06 §6)", () => {
  it("folds a full turn in contract order", () => {
    const [t] = run([
      { type: "start", key: "k", prompt: "Dune?", at: 0 },
      ev("meta", { conversation_id: "c1", turn: 1, model: "m", locale: "en" }),
      ev("tool_call", { id: "call_1", name: "search_catalog", args: { query: "Dune" } }),
      ev("tool_result", { id: "call_1", summary: "1 book found" }),
      ev("token", { text: "From " }, 5),
      ev("token", { text: "Dune." }, 6),
      ev("citations", {
        citations: [{ book_id: "b", title: "Dune", path: null, snippet: "", score: 0.9 }],
      }),
      ev("done", { message_id: "m", tokens_in: 1, tokens_out: 2, finish_reason: "stop" }, 9),
    ]);
    expect(t.status).toBe("done");
    expect(t.text).toBe("From Dune.");
    expect(t.firstTokenAt).toBe(5);
    expect(t.finishedAt).toBe(9);
    expect(t.meta?.conversation_id).toBe("c1");
    expect(t.tools).toEqual([
      { id: "call_1", name: "search_catalog", args: { query: "Dune" }, summary: "1 book found" },
    ]);
    expect(t.citations).toHaveLength(1);
  });

  it("error replaces the tail and keeps partial tokens", () => {
    const [t] = run([
      { type: "start", key: "k", prompt: "x", at: 0 },
      ev("token", { text: "Part" }),
      ev("error", { code: "UPSTREAM_ERROR", message: "boom" }),
      ev("token", { text: "ignored" }),
    ]);
    expect(t.status).toBe("error");
    expect(t.text).toBe("Part");
    expect(t.error).toEqual({ code: "UPSTREAM_ERROR", message: "boom" });
  });

  it("marks a stream that ends without done as interrupted, abort as aborted", () => {
    const [a] = run([
      { type: "start", key: "k", prompt: "x", at: 0 },
      { type: "end", key: "k", at: 1 },
    ]);
    expect(a.error?.code).toBe(STREAM_INTERRUPTED);
    const [b] = run([
      { type: "start", key: "k", prompt: "x", at: 0 },
      { type: "abort", key: "k", at: 1 },
      { type: "end", key: "k", at: 2 },
    ]);
    expect(b.status).toBe("aborted");
  });

  it("records pre-stream failures with rate-limit extras", () => {
    const [t] = run([
      { type: "start", key: "k", prompt: "x", at: 0 },
      { type: "fail", key: "k", error: { code: "RATE_LIMITED", message: "cap", limit: 40 }, at: 1 },
    ]);
    expect(t.status).toBe("error");
    expect(t.error?.limit).toBe(40);
  });
});

describe("turnsFromHistory", () => {
  it("groups user → tool → assistant rows", () => {
    const turns = turnsFromHistory([
      { id: "1", role: "user", content: "Dune?", tool_meta: null, created_at: "" },
      {
        id: "2",
        role: "tool",
        content: "{}",
        tool_meta: { tool_call_id: "c", name: "search_catalog", args: {}, result: "{}" },
        created_at: "",
      },
      { id: "3", role: "assistant", content: "Dune is on 3F.", tool_meta: null, created_at: "" },
      { id: "4", role: "user", content: "Thanks", tool_meta: null, created_at: "" },
    ]);
    expect(turns).toHaveLength(2);
    expect(turns[0]).toMatchObject({ prompt: "Dune?", text: "Dune is on 3F.", historical: true });
    expect(turns[0].tools[0].name).toBe("search_catalog");
    expect(turns[1]).toMatchObject({ prompt: "Thanks", text: "" });
  });
});

describe("parseShelfPath", () => {
  it("parses the docs/05 §3 token", () => {
    expect(parseShelfPath("MAIN/3F/SF/A-1/S-2")).toEqual({
      building: "MAIN",
      floor: 3,
      section: "SF",
      aisle: "A-1",
      shelf: "S-2",
    });
    expect(parseShelfPath("MAIN / 0F / REF / A-9 / S-1")?.floor).toBe(0);
  });

  it("rejects malformed paths", () => {
    expect(parseShelfPath(null)).toBeNull();
    expect(parseShelfPath("MAIN/3F")).toBeNull();
    expect(parseShelfPath("MAIN/X/SF/A-1/S-2")).toBeNull();
  });
});
