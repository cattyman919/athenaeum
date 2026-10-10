// Pure chat-turn state machine for the /chat SSE client (docs/06 §6, docs/07 §6).
// Event order within one turn is fixed: meta → (tool_call → tool_result)* → token* →
// citations → done; `error` replaces the remaining flow. The hook feeds parsed frames
// here so the logic stays unit-testable without a browser.

import type {
  ChatCitation,
  ChatDoneEvent,
  ChatErrorEvent,
  ChatMessage,
  ChatMetaEvent,
  ChatToolCallEvent,
  ChatToolResultEvent,
} from "./types";

export interface ToolActivity {
  id: string;
  name: string;
  args: unknown;
  summary?: string;
}

export interface TurnError {
  code: string;
  message: string;
  /** Extra `limit` from the 40-message cap 429 (docs/06 §8). */
  limit?: number;
  /** Epoch seconds from X-RateLimit-Reset on a 429. */
  resetAt?: number;
}

export type TurnStatus = "streaming" | "done" | "error" | "aborted";

export interface ChatTurn {
  key: string;
  prompt: string;
  text: string;
  tools: ToolActivity[];
  citations: ChatCitation[];
  meta?: ChatMetaEvent;
  done?: ChatDoneEvent;
  error?: TurnError;
  status: TurnStatus;
  /** Loaded from GET /api/chat/conversations/{id} rather than streamed now. */
  historical?: boolean;
  startedAt?: number;
  firstTokenAt?: number;
  finishedAt?: number;
}

export type ChatAction =
  | { type: "start"; key: string; prompt: string; at: number }
  | { type: "event"; key: string; event: string; data: unknown; at: number }
  | { type: "fail"; key: string; error: TurnError; at: number }
  | { type: "abort"; key: string; at: number }
  | { type: "end"; key: string; at: number }
  | { type: "reset"; turns: ChatTurn[] };

/** Code used when the stream closes without a terminal `done`/`error` event. */
export const STREAM_INTERRUPTED = "STREAM_INTERRUPTED";

function applyEvent(turn: ChatTurn, event: string, data: unknown, at: number): ChatTurn {
  if (turn.status !== "streaming") return turn; // nothing follows done/error/abort
  switch (event) {
    case "meta":
      return { ...turn, meta: data as ChatMetaEvent };
    case "tool_call": {
      const d = data as ChatToolCallEvent;
      return { ...turn, tools: [...turn.tools, { id: d.id, name: d.name, args: d.args }] };
    }
    case "tool_result": {
      const d = data as ChatToolResultEvent;
      return {
        ...turn,
        tools: turn.tools.map((t) => (t.id === d.id ? { ...t, summary: d.summary } : t)),
      };
    }
    case "token": {
      const text = (data as { text?: string }).text ?? "";
      return { ...turn, text: turn.text + text, firstTokenAt: turn.firstTokenAt ?? at };
    }
    case "citations":
      return { ...turn, citations: (data as { citations?: ChatCitation[] }).citations ?? [] };
    case "done":
      return { ...turn, done: data as ChatDoneEvent, status: "done", finishedAt: at };
    case "error": {
      const d = data as ChatErrorEvent;
      return {
        ...turn,
        error: { code: d.code, message: d.message },
        status: "error",
        finishedAt: at,
      };
    }
    default:
      return turn; // unknown events are ignored (forward compatible)
  }
}

function update(turns: ChatTurn[], key: string, fn: (t: ChatTurn) => ChatTurn): ChatTurn[] {
  return turns.map((t) => (t.key === key ? fn(t) : t));
}

export function chatReducer(turns: ChatTurn[], action: ChatAction): ChatTurn[] {
  switch (action.type) {
    case "reset":
      return action.turns;
    case "start":
      return [
        ...turns,
        {
          key: action.key,
          prompt: action.prompt,
          text: "",
          tools: [],
          citations: [],
          status: "streaming",
          startedAt: action.at,
        },
      ];
    case "event":
      return update(turns, action.key, (t) => applyEvent(t, action.event, action.data, action.at));
    case "fail":
      return update(turns, action.key, (t) =>
        t.status === "streaming"
          ? { ...t, status: "error", error: action.error, finishedAt: action.at }
          : t,
      );
    case "abort":
      return update(turns, action.key, (t) =>
        t.status === "streaming" ? { ...t, status: "aborted", finishedAt: action.at } : t,
      );
    case "end":
      return update(turns, action.key, (t) =>
        t.status === "streaming"
          ? {
              ...t,
              status: "error",
              error: { code: STREAM_INTERRUPTED, message: "stream closed early" },
              finishedAt: action.at,
            }
          : t,
      );
  }
}

/** Rebuilds display turns from persisted messages: user → tool rows* → assistant. */
export function turnsFromHistory(messages: ChatMessage[]): ChatTurn[] {
  const turns: ChatTurn[] = [];
  let current: ChatTurn | null = null;
  for (const m of messages) {
    if (m.role === "user") {
      current = {
        key: m.id,
        prompt: m.content ?? "",
        text: "",
        tools: [],
        citations: [],
        status: "done",
        historical: true,
      };
      turns.push(current);
    } else if (!current) {
      continue; // system rows or orphans before the first user message
    } else if (m.role === "tool" && m.tool_meta) {
      current.tools.push({
        id: m.tool_meta.tool_call_id,
        name: m.tool_meta.name,
        args: m.tool_meta.args,
      });
    } else if (m.role === "assistant") {
      current.text = current.text ? `${current.text}\n\n${m.content ?? ""}` : (m.content ?? "");
    }
  }
  return turns;
}

/** Splits a stored shelf path token (`MAIN/3F/SF/A-1/S-2`, docs/05 §3) into its codes. */
export function parseShelfPath(path: string | null | undefined) {
  if (!path) return null;
  const parts = path.split("/").map((p) => p.trim());
  if (parts.length < 5) return null;
  const [building, floor, section, aisle, shelf] = parts;
  const n = Number.parseInt(floor, 10);
  if (!/^\d+F$/i.test(floor) || Number.isNaN(n)) return null;
  return { building, floor: n, section, aisle, shelf };
}
