"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useFormatter, useLocale, useTranslations } from "next-intl";
import {
  BookOpenCheck,
  Check,
  Copy,
  Loader2,
  MessageSquarePlus,
  Mic,
  RotateCcw,
  SendHorizontal,
  Sparkles,
  Square,
  Trash2,
  Wrench,
} from "lucide-react";
import { toast } from "sonner";
import { AICitationCard } from "@/components/ai-citation-card";
import { ChatTelemetry } from "@/components/chat-telemetry";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { useChatStream } from "@/hooks/use-chat-stream";
import { useVoiceInput } from "@/hooks/use-voice-input";
import { api } from "@/lib/api-client";
import { STREAM_INTERRUPTED, type ChatTurn, type TurnError } from "@/lib/chat-stream";
import type { Locale } from "@/lib/locale";
import type { ConversationSummary } from "@/lib/types";
import { cn } from "@/lib/utils";
import { formatElapsed, mergeTranscript, type VoiceError } from "@/lib/voice";

function CopyReplyButton({ text }: { text: string }) {
  const t = useTranslations("chat.thread");
  const [copied, setCopied] = React.useState(false);
  React.useEffect(() => {
    if (!copied) return;
    const id = window.setTimeout(() => setCopied(false), 1_800);
    return () => window.clearTimeout(id);
  }, [copied]);
  return (
    <button
      type="button"
      data-testid="chat-copy"
      onClick={() => {
        void navigator.clipboard?.writeText(text).then(() => setCopied(true));
      }}
      className="mt-2 inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {copied ? (
        <Check className="h-3 w-3" aria-hidden />
      ) : (
        <Copy className="h-3 w-3" aria-hidden />
      )}
      <span aria-live="polite">{copied ? t("copied") : t("copy")}</span>
    </button>
  );
}

const KNOWN_TOOLS = [
  "search_catalog",
  "get_book_location",
  "check_availability",
  "recommend_similar",
] as const;
const SUGGESTIONS = ["babylonian", "scifi", "availability"] as const;

/** Mirrors the server title rule (docs/06 §7): first 6 words + "…". */
function provisionalTitle(prompt: string): string {
  const words = prompt.trim().split(/\s+/);
  return words.length > 6 ? `${words.slice(0, 6).join(" ")}…` : prompt.trim();
}

export interface ChatWorkspaceProps {
  conversations: ConversationSummary[];
  activeId?: string;
  initialTurns: ChatTurn[];
  loadError?: boolean;
  /** GET /api/chat/conversations failed (docs/07 §9) — the list shows an inline error. */
  listError?: boolean;
  prefill?: string;
}

/** Screen 5 — two-pane AI Librarian workspace (docs/07 §7, docs/12 screen 5). */
export function ChatWorkspace(props: ChatWorkspaceProps) {
  const t = useTranslations("chat");
  const router = useRouter();
  const [list, setList] = React.useState(props.conversations);
  const [syncedFrom, setSyncedFrom] = React.useState(props.conversations);
  const [currentId, setCurrentId] = React.useState(props.activeId);
  const [syncedActive, setSyncedActive] = React.useState(props.activeId);
  const [nonce, setNonce] = React.useState(0);

  // Server navigations deliver fresh props; adopt them (render-time sync, no effect).
  if (syncedFrom !== props.conversations) {
    setSyncedFrom(props.conversations);
    setList(props.conversations);
  }
  if (syncedActive !== props.activeId) {
    setSyncedActive(props.activeId);
    setCurrentId(props.activeId);
  }

  const startNew = React.useCallback(() => {
    setNonce((n) => n + 1);
    setCurrentId(undefined);
    router.push("/chat");
  }, [router]);

  const onConversation = React.useCallback((id: string, prompt: string) => {
    setCurrentId(id);
    setList((prev) => {
      const now = new Date().toISOString();
      const existing = prev.find((c) => c.id === id);
      const item = existing
        ? { ...existing, updated_at: now }
        : { id, title: provisionalTitle(prompt), updated_at: now };
      return [item, ...prev.filter((c) => c.id !== id)];
    });
    // Keep the URL shareable without a server round trip (URL-as-state, docs/07 §5).
    if (new URLSearchParams(window.location.search).get("c") !== id) {
      window.history.replaceState(null, "", `/chat?c=${encodeURIComponent(id)}`);
    }
  }, []);

  const remove = async (c: ConversationSummary) => {
    try {
      await api.deleteConversation(c.id);
      setList((prev) => prev.filter((x) => x.id !== c.id));
      toast.success(t("conversations.deleted"));
      if (c.id === currentId) startNew();
    } catch {
      toast.error(t("conversations.deleteError"));
    }
  };

  return (
    <div className="flex h-[calc(100vh-12rem)] min-h-[34rem] overflow-hidden rounded-lg border border-border bg-card shadow-sm">
      <aside
        aria-label={t("conversations.heading")}
        className="hidden w-72 shrink-0 flex-col border-r border-border bg-background/60 md:flex"
      >
        <div className="flex items-center justify-between gap-2 border-b border-border p-3">
          <h2 className="text-sm font-semibold">{t("conversations.heading")}</h2>
          <Button size="sm" variant="outline" onClick={startNew} data-testid="chat-new">
            <MessageSquarePlus className="h-4 w-4" aria-hidden />
            {t("conversations.new")}
          </Button>
        </div>
        <ul className="flex-1 overflow-y-auto p-2" data-testid="conversation-list">
          {list.length === 0 &&
            (props.listError ? (
              <li
                role="alert"
                data-testid="conversation-list-error"
                className="px-2 py-6 text-center text-xs text-destructive"
              >
                {t("conversations.listError")}
              </li>
            ) : (
              <li className="px-2 py-6 text-center text-xs text-muted-foreground">
                {t("conversations.empty")}
              </li>
            ))}
          {list.map((c) => {
            const active = c.id === currentId;
            const title = c.title || t("conversations.untitled");
            return (
              <li key={c.id} className="group relative" data-testid="conversation-item">
                <Link
                  href={`/chat?c=${encodeURIComponent(c.id)}`}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "block truncate rounded-md py-2 pl-3 pr-9 text-sm transition-colors hover:bg-secondary",
                    active ? "bg-secondary font-medium text-foreground" : "text-muted-foreground",
                  )}
                >
                  {title}
                </Link>
                <button
                  type="button"
                  aria-label={t("conversations.delete", { title })}
                  onClick={() => void remove(c)}
                  className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:text-destructive focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring group-hover:opacity-100"
                >
                  <Trash2 className="h-4 w-4" aria-hidden />
                </button>
              </li>
            );
          })}
        </ul>
      </aside>

      <ChatSession
        key={`${props.activeId ?? "new"}:${nonce}`}
        conversationId={props.activeId}
        initialTurns={props.initialTurns}
        loadError={props.loadError}
        prefill={props.prefill}
        onConversation={onConversation}
        onNew={startNew}
      />
    </div>
  );
}

function ChatSession({
  conversationId,
  initialTurns,
  loadError,
  prefill,
  onConversation,
  onNew,
}: {
  conversationId?: string;
  initialTurns: ChatTurn[];
  loadError?: boolean;
  prefill?: string;
  onConversation: (id: string, prompt: string) => void;
  onNew: () => void;
}) {
  const t = useTranslations("chat");
  const format = useFormatter();
  const locale = useLocale() as Locale;
  const [draft, setDraft] = React.useState(prefill ?? "");
  const lastPrompt = React.useRef("");
  const threadRef = React.useRef<HTMLDivElement>(null);
  const toasted = React.useRef(new Set<string>());
  const composerRef = React.useRef<HTMLTextAreaElement>(null);
  const tv = useTranslations("voice");
  const [voiceNote, setVoiceNote] = React.useState<{ text: string; tone: "info" | "warn" } | null>(
    null,
  );

  const voiceErrorText = React.useCallback(
    (e: VoiceError) => {
      if (e.kind === "rateLimited") {
        return e.retryAfter
          ? tv("errors.rateLimited", { seconds: e.retryAfter })
          : tv("errors.rateLimitedSoon");
      }
      return tv(`errors.${e.kind}`);
    },
    [tv],
  );

  // Push-to-talk (docs/15 §2): the transcript lands beside typed text; the user presses Send.
  const voice = useVoiceInput({
    locale,
    unnamedMic: (n) => tv("mic.unnamed", { n }),
    onTranscript: (text) => {
      if (!text) {
        setVoiceNote({ text: tv("empty"), tone: "warn" });
        return;
      }
      setDraft((d) => mergeTranscript(d, text));
      setVoiceNote({ text: tv("inserted"), tone: "info" });
      composerRef.current?.focus();
    },
    onError: (e) => {
      const text = voiceErrorText(e);
      setVoiceNote({ text, tone: "warn" });
      toast.error(text);
    },
  });

  const { turns, streaming, quota, send, abort } = useChatStream({
    conversationId,
    initialTurns,
    locale,
    onMeta: (meta) => onConversation(meta.conversation_id, lastPrompt.current),
  });

  const submit = (text: string) => {
    const msg = text.trim();
    if (!msg || streaming || voice.busy) return;
    setVoiceNote(null);
    lastPrompt.current = msg;
    setDraft("");
    void send(msg);
  };

  const errorText = React.useCallback(
    (err: TurnError) => {
      if (err.code === "RATE_LIMITED") {
        if (err.limit) return t("errors.cap", { limit: err.limit });
        if (err.resetAt) {
          return t("errors.rateLimited", {
            when: format.relativeTime(new Date(err.resetAt * 1000), new Date()),
          });
        }
        return t("errors.rateLimitedSoon");
      }
      if (err.code === "UPSTREAM_ERROR" || err.code.endsWith("_UNAVAILABLE")) {
        return t("errors.upstream");
      }
      if (err.code === "NETWORK_ERROR") return t("errors.network");
      if (err.code === STREAM_INTERRUPTED) return t("errors.interrupted");
      return t("errors.generic");
    },
    [t, format],
  );

  // Toast each failed turn once (docs/07 §9: toast + retry, partial tokens stay).
  React.useEffect(() => {
    for (const turn of turns) {
      if (turn.status === "error" && turn.error && !toasted.current.has(turn.key)) {
        toasted.current.add(turn.key);
        toast.error(errorText(turn.error));
      }
    }
  }, [turns, errorText]);

  // Follow the stream.
  React.useEffect(() => {
    const el = threadRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [turns]);

  const latest = [...turns].reverse().find((x) => !x.historical);
  const last = turns[turns.length - 1];
  const status = !last
    ? ""
    : last.status === "streaming"
      ? t("thread.streaming")
      : last.status === "aborted"
        ? t("thread.stopped")
        : last.status === "done" && !last.historical
          ? t("thread.complete")
          : "";

  return (
    <section className="flex min-w-0 flex-1 flex-col" aria-labelledby="chat-heading">
      <h2 id="chat-heading" className="sr-only">
        {t("thread.label")}
      </h2>
      <ChatTelemetry turn={latest} quota={quota} />

      <div
        ref={threadRef}
        role="log"
        aria-label={t("thread.label")}
        data-testid="chat-thread"
        className="flex-1 overflow-y-auto px-4 py-6 md:px-8"
      >
        {loadError && (
          <p
            role="alert"
            data-testid="chat-load-error"
            className="mb-4 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
          >
            {t("conversations.loadError")}
          </p>
        )}
        {turns.length === 0 ? (
          <div className="mx-auto flex max-w-xl flex-col items-center gap-4 py-12 text-center">
            <span className="flex h-12 w-12 items-center justify-center rounded-full bg-accent/15 text-accent">
              <Sparkles className="h-6 w-6" aria-hidden />
            </span>
            <h3 className="text-lg font-semibold">{t("empty.title")}</h3>
            <p className="text-sm text-muted-foreground">{t("empty.body")}</p>
            <div className="flex flex-wrap justify-center gap-2">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  data-testid={`suggestion-${s}`}
                  onClick={() => submit(t(`empty.suggestions.${s}`))}
                  className="rounded-full border border-border bg-card px-3 py-1.5 text-xs text-foreground shadow-sm transition-colors hover:border-primary/40 hover:bg-secondary"
                >
                  {t(`empty.suggestions.${s}`)}
                </button>
              ))}
            </div>
          </div>
        ) : (
          <ol className="mx-auto flex max-w-3xl flex-col gap-6">
            {turns.map((turn, i) => (
              <TurnView
                key={turn.key}
                turn={turn}
                isLast={i === turns.length - 1}
                errorText={errorText}
                onRetry={() => submit(turn.prompt)}
                onNew={onNew}
                retryDisabled={streaming}
              />
            ))}
          </ol>
        )}
      </div>

      <p className="sr-only" aria-live="polite" data-testid="chat-status">
        {status}
      </p>

      <form
        className="border-t border-border bg-card p-3"
        onSubmit={(e) => {
          e.preventDefault();
          submit(draft);
        }}
      >
        <div className="flex items-end gap-2 rounded-lg border border-border bg-background p-2 focus-within:ring-2 focus-within:ring-ring">
          <label htmlFor="chat-composer" className="sr-only">
            {t("composer.label")}
          </label>
          <textarea
            ref={composerRef}
            id="chat-composer"
            data-testid="chat-composer"
            rows={2}
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value);
              if (voiceNote?.tone === "info") setVoiceNote(null);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                submit(draft);
              }
            }}
            placeholder={t("composer.placeholder")}
            className="max-h-40 min-h-[2.5rem] flex-1 resize-none bg-transparent px-2 py-1.5 text-sm leading-relaxed placeholder:text-muted-foreground focus:outline-none"
          />
          {voice.supported && (
            <Button
              type="button"
              size="icon"
              variant={voice.recording ? "destructive" : "outline"}
              onClick={voice.toggle}
              onKeyDown={(e) => {
                if (e.key === "Escape" && voice.recording) {
                  e.preventDefault();
                  voice.cancel();
                }
              }}
              disabled={voice.status === "requesting" || voice.status === "transcribing"}
              aria-pressed={voice.recording}
              aria-label={voice.recording ? tv("stop") : tv("start")}
              title={voice.recording ? tv("stop") : tv("start")}
              data-testid="chat-mic"
              data-state={voice.status}
              className="h-10 w-10 shrink-0"
            >
              {voice.status === "transcribing" || voice.status === "requesting" ? (
                <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" aria-hidden />
              ) : voice.recording ? (
                <Square className="h-4 w-4 fill-current" aria-hidden />
              ) : (
                <Mic className="h-4 w-4" aria-hidden />
              )}
            </Button>
          )}
          {streaming ? (
            <Button type="button" variant="outline" onClick={abort} data-testid="chat-stop">
              <Square className="h-4 w-4" aria-hidden />
              {t("composer.stop")}
            </Button>
          ) : (
            <Button type="submit" disabled={!draft.trim() || voice.busy} data-testid="chat-send">
              <SendHorizontal className="h-4 w-4" aria-hidden />
              {t("composer.send")}
            </Button>
          )}
        </div>
        {/* Live interim transcript (docs/15 §2a): muted preview, never written into the draft;
            the stop-time final pass replaces it. */}
        {voice.interim && (voice.recording || voice.status === "transcribing") && (
          <p
            data-testid="chat-voice-interim"
            className="mt-1.5 line-clamp-2 px-3 text-sm italic text-muted-foreground"
          >
            <span className="sr-only">{tv("interim")}: </span>
            {voice.interim}
          </p>
        )}
        {voice.supported && (
          <div className="mt-1.5 flex flex-wrap items-center gap-2 px-1 text-[11px] text-muted-foreground">
            <label htmlFor="chat-mic-select">{tv("mic.label")}</label>
            <select
              id="chat-mic-select"
              data-testid="chat-mic-select"
              value={voice.mics.some((m) => m.deviceId === voice.deviceId) ? voice.deviceId : ""}
              onChange={(e) =>
                voice.setDeviceId(
                  e.target.value,
                  voice.mics.find((m) => m.deviceId === e.target.value)?.label ?? "",
                )
              }
              disabled={voice.busy}
              className="max-w-[16rem] truncate rounded-md border border-border bg-background px-1.5 py-1 text-[11px] text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <option value="">{tv("mic.default")}</option>
              {voice.mics.map((m) => (
                <option key={m.deviceId} value={m.deviceId}>
                  {m.label}
                </option>
              ))}
            </select>
            {voice.recording && (
              <span
                role="meter"
                aria-label={tv("mic.level")}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(voice.level * 100)}
                data-testid="chat-mic-level"
                className="relative h-1.5 w-20 overflow-hidden rounded-full bg-secondary"
              >
                <span
                  className="absolute inset-y-0 left-0 rounded-full bg-primary transition-[width] duration-100 motion-reduce:transition-none"
                  style={{ width: `${Math.round(Math.min(1, voice.level * 3) * 100)}%` }}
                />
              </span>
            )}
          </div>
        )}
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 px-1 text-[11px]">
          {voice.recording ? (
            <span
              role="status"
              data-testid="chat-recording"
              className="inline-flex items-center gap-1.5 font-medium text-destructive"
            >
              <span
                aria-hidden
                className="h-2 w-2 animate-pulse rounded-full bg-destructive motion-reduce:animate-none"
              />
              {tv("recording", { elapsed: formatElapsed(voice.elapsedMs) })}
            </span>
          ) : voice.status === "requesting" || voice.status === "transcribing" ? (
            <span role="status" data-testid="chat-voice-busy" className="text-muted-foreground">
              {tv(voice.status)}
            </span>
          ) : (
            <span className="text-muted-foreground">{t("composer.hint")}</span>
          )}
          <span
            aria-live="polite"
            data-testid="chat-voice-note"
            className={cn(
              voiceNote?.tone === "warn" ? "text-warning" : "text-muted-foreground",
              !voiceNote && "sr-only",
            )}
          >
            {voice.busy ? "" : (voiceNote?.text ?? "")}
          </span>
        </div>
      </form>
    </section>
  );
}

function TurnView({
  turn,
  isLast,
  errorText,
  onRetry,
  onNew,
  retryDisabled,
}: {
  turn: ChatTurn;
  isLast: boolean;
  errorText: (e: TurnError) => string;
  onRetry: () => void;
  onNew: () => void;
  retryDisabled: boolean;
}) {
  const t = useTranslations("chat");
  const streaming = turn.status === "streaming";
  const toolLabel = (name: string) =>
    (KNOWN_TOOLS as readonly string[]).includes(name)
      ? t(`tools.${name as (typeof KNOWN_TOOLS)[number]}`)
      : t("tools.unknown", { name });

  return (
    <li className="flex flex-col gap-3" data-testid="chat-turn-item" data-status={turn.status}>
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-lg rounded-br-sm bg-primary px-4 py-2.5 text-sm leading-relaxed text-primary-foreground">
          <span className="sr-only">{t("thread.you")}: </span>
          <span className="whitespace-pre-wrap" data-testid="chat-user-message">
            {turn.prompt}
          </span>
        </div>
      </div>

      <div className="flex gap-3">
        <span
          aria-hidden
          className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-accent/15 text-accent"
        >
          <BookOpenCheck className="h-4 w-4" />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <span className="text-xs font-medium text-muted-foreground">{t("thread.librarian")}</span>

          {turn.tools.length > 0 && (
            <ul
              aria-label={t("tools.label")}
              className="flex flex-wrap gap-1.5"
              data-testid="tool-activity"
            >
              {turn.tools.map((tool) => (
                <li
                  key={tool.id}
                  data-testid="tool-chip"
                  className="inline-flex items-center gap-1.5 rounded-full border border-border bg-secondary px-2.5 py-1 text-[11px] text-muted-foreground"
                >
                  <Wrench className="h-3 w-3" aria-hidden />
                  <span className="text-foreground">{toolLabel(tool.name)}</span>
                  {tool.summary ? (
                    <span className="font-mono">· {tool.summary}</span>
                  ) : (
                    !turn.historical &&
                    streaming && <span className="italic">· {t("tools.running")}</span>
                  )}
                </li>
              ))}
            </ul>
          )}

          {(turn.text || streaming) && (
            <div className="rounded-lg rounded-tl-sm border border-border bg-background px-4 py-3 text-sm leading-relaxed">
              {turn.text ? (
                <>
                  <div data-testid="chat-assistant-message">
                    <Markdown>{turn.text}</Markdown>
                    {streaming && (
                      <span
                        aria-hidden
                        className="ml-0.5 inline-block h-4 w-1.5 animate-pulse bg-primary/60 align-text-bottom motion-reduce:animate-none"
                      />
                    )}
                  </div>
                  {!streaming && <CopyReplyButton text={turn.text} />}
                </>
              ) : (
                <p className="flex items-center gap-2 text-muted-foreground">
                  <Loader2
                    className="h-4 w-4 animate-spin motion-reduce:animate-none"
                    aria-hidden
                  />
                  {t("thread.thinking")}
                </p>
              )}
            </div>
          )}

          {turn.citations.length > 0 && (
            <section aria-label={t("citations.heading")} className="flex flex-col gap-2">
              <h4 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {t("citations.heading")}
              </h4>
              <div className="grid gap-2 sm:grid-cols-2" data-testid="citations">
                {turn.citations.map((c) => (
                  <AICitationCard key={c.book_id} citation={c} />
                ))}
              </div>
            </section>
          )}

          {turn.done?.finish_reason === "rate_limited_daily" && (
            <p className="rounded-md border border-warning/30 bg-warning/5 p-2 text-xs text-warning">
              {t("thread.dailyExhausted")}
            </p>
          )}

          {turn.status === "aborted" && (
            <p className="text-xs italic text-muted-foreground">{t("thread.stopped")}</p>
          )}

          {turn.status === "error" && turn.error && (
            <div
              role="alert"
              data-testid="chat-error"
              className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs text-destructive"
            >
              <span className="flex-1">{errorText(turn.error)}</span>
              {turn.error.limit ? (
                <Button size="sm" variant="outline" onClick={onNew}>
                  <MessageSquarePlus className="h-3.5 w-3.5" aria-hidden />
                  {t("errors.startNew")}
                </Button>
              ) : (
                isLast && (
                  <Button size="sm" variant="outline" onClick={onRetry} disabled={retryDisabled}>
                    <RotateCcw className="h-3.5 w-3.5" aria-hidden />
                    {t("errors.retry")}
                  </Button>
                )
              )}
            </div>
          )}
        </div>
      </div>
    </li>
  );
}
