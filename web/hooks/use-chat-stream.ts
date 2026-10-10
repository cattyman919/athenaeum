"use client";

// Chat streaming client (docs/07 §6): POST /api/chat via fetch + ReadableStream (EventSource
// cannot POST), incremental SSE parsing, abort, and rate-limit quota tracking
// (5/min + 50/day — docs/03 §2). Event contract: docs/06 §6.

import * as React from "react";
import { api, toError } from "@/lib/api-client";
import { chatReducer, type ChatTurn, type TurnError } from "@/lib/chat-stream";
import type { Locale } from "@/lib/locale";
import { createSSEParser, type SSEFrame } from "@/lib/sse";
import type { ChatDoneEvent, ChatMetaEvent } from "@/lib/types";

export const MINUTE_LIMIT = 5;
export const DAILY_LIMIT = 50;

export interface QuotaState {
  /** Turns sent in the trailing 60 s (client-side window; the server bucket is authoritative). */
  minuteUsed: number;
  /** Remaining daily turns from X-RateLimit-* (the 50/24h bucket's headers), once known. */
  dailyRemaining: number | null;
  /** Epoch seconds when the binding bucket resets, from the last 429. */
  blockedUntil: number | null;
}

export interface UseChatStreamOptions {
  conversationId?: string;
  initialTurns?: ChatTurn[];
  locale: Locale;
  /** Fired on the first `meta` of a turn (e.g. a new conversation was created). */
  onMeta?: (meta: ChatMetaEvent) => void;
  /** Fired once a turn settles (done, error, abort). */
  onSettled?: (turn: { done?: ChatDoneEvent; conversationId?: string }) => void;
  /** Injectable for tests. */
  request?: (path: string, init: RequestInit) => Promise<Response>;
}

let seq = 0;
const nextKey = () => `turn-${Date.now().toString(36)}-${(seq++).toString(36)}`;

function readHeaderInt(h: Headers, name: string): number | null {
  const v = h.get(name);
  if (v === null) return null;
  const n = Number.parseInt(v, 10);
  return Number.isNaN(n) ? null : n;
}

export function useChatStream(opts: UseChatStreamOptions) {
  const [turns, dispatch] = React.useReducer(chatReducer, opts.initialTurns ?? []);
  const [streaming, setStreaming] = React.useState(false);
  const [conversationId, setConversationId] = React.useState(opts.conversationId);
  const [quota, setQuota] = React.useState<QuotaState>({
    minuteUsed: 0,
    dailyRemaining: null,
    blockedUntil: null,
  });

  const convRef = React.useRef(opts.conversationId);
  const ctrlRef = React.useRef<AbortController | null>(null);
  const sentAt = React.useRef<number[]>([]);
  const optsRef = React.useRef(opts);
  React.useEffect(() => {
    optsRef.current = opts;
  });

  // Recompute the sliding per-minute window as old sends age out.
  React.useEffect(() => {
    const id = window.setInterval(() => {
      const cutoff = Date.now() - 60_000;
      const before = sentAt.current.length;
      sentAt.current = sentAt.current.filter((t) => t > cutoff);
      setQuota((q) => {
        const blocked =
          q.blockedUntil && q.blockedUntil * 1000 <= Date.now() ? null : q.blockedUntil;
        if (before === sentAt.current.length && blocked === q.blockedUntil) return q;
        return { ...q, minuteUsed: sentAt.current.length, blockedUntil: blocked };
      });
    }, 1_000);
    return () => window.clearInterval(id);
  }, []);

  // Abort an in-flight turn when the page unmounts.
  React.useEffect(() => () => ctrlRef.current?.abort(), []);

  const readQuota = React.useCallback((h: Headers, status: number) => {
    const limit = readHeaderInt(h, "X-RateLimit-Limit");
    const remaining = readHeaderInt(h, "X-RateLimit-Remaining");
    const reset = readHeaderInt(h, "X-RateLimit-Reset");
    setQuota((q) => {
      const next = { ...q };
      if (limit === DAILY_LIMIT && remaining !== null) next.dailyRemaining = remaining;
      if (status === 429) {
        if (limit === MINUTE_LIMIT) next.minuteUsed = MINUTE_LIMIT;
        if (limit === DAILY_LIMIT) next.dailyRemaining = 0;
        next.blockedUntil = reset;
      }
      return next;
    });
  }, []);

  const abort = React.useCallback(() => {
    ctrlRef.current?.abort();
  }, []);

  const send = React.useCallback(
    async (message: string) => {
      const text = message.trim();
      if (!text || ctrlRef.current) return;
      const key = nextKey();
      const ctrl = new AbortController();
      ctrlRef.current = ctrl;
      setStreaming(true);
      dispatch({ type: "start", key, prompt: text, at: Date.now() });
      sentAt.current.push(Date.now());
      setQuota((q) => ({ ...q, minuteUsed: sentAt.current.length }));

      const request = optsRef.current.request ?? api.request;
      let done: ChatDoneEvent | undefined;
      const handle = (frame: SSEFrame) => {
        let data: unknown;
        try {
          data = JSON.parse(frame.data);
        } catch {
          return; // malformed frame — skip rather than kill the turn
        }
        if (frame.event === "meta") {
          const meta = data as ChatMetaEvent;
          if (meta.conversation_id && meta.conversation_id !== convRef.current) {
            convRef.current = meta.conversation_id;
            setConversationId(meta.conversation_id);
          }
          optsRef.current.onMeta?.(meta);
        } else if (frame.event === "done") {
          done = data as ChatDoneEvent;
        }
        dispatch({ type: "event", key, event: frame.event, data, at: Date.now() });
      };

      try {
        const body: Record<string, string> = { message: text, locale: optsRef.current.locale };
        if (convRef.current) body.conversation_id = convRef.current;
        const res = await request("/api/chat", {
          method: "POST",
          headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
          body: JSON.stringify(body),
          signal: ctrl.signal,
        });
        readQuota(res.headers, res.status);
        if (!res.ok || !res.body) {
          // Errors before the first byte use the JSON envelope (docs/03 §7.1).
          const err = await toError(res);
          const failure: TurnError = { code: err.code, message: err.message };
          if (typeof err.body?.limit === "number") failure.limit = err.body.limit;
          const reset = readHeaderInt(res.headers, "X-RateLimit-Reset");
          if (res.status === 429 && reset !== null) failure.resetAt = reset;
          dispatch({ type: "fail", key, error: failure, at: Date.now() });
          return;
        }
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        const parser = createSSEParser();
        for (;;) {
          const { value, done: eof } = await reader.read();
          if (eof) break;
          for (const f of parser.push(decoder.decode(value, { stream: true }))) handle(f);
        }
        for (const f of parser.push(decoder.decode())) handle(f);
        for (const f of parser.end()) handle(f);
        dispatch({ type: "end", key, at: Date.now() });
      } catch (e) {
        if (ctrl.signal.aborted) {
          dispatch({ type: "abort", key, at: Date.now() });
        } else {
          const message = e instanceof Error ? e.message : String(e);
          dispatch({
            type: "fail",
            key,
            error: { code: "NETWORK_ERROR", message },
            at: Date.now(),
          });
        }
      } finally {
        ctrlRef.current = null;
        setStreaming(false);
        optsRef.current.onSettled?.({ done, conversationId: convRef.current });
      }
    },
    [readQuota],
  );

  return { turns, streaming, conversationId, quota, send, abort };
}
