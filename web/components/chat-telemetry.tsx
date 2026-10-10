"use client";

import { useTranslations } from "next-intl";
import { Activity, Gauge } from "lucide-react";
import { DAILY_LIMIT, MINUTE_LIMIT, type QuotaState } from "@/hooks/use-chat-stream";
import type { ChatTurn } from "@/lib/chat-stream";
import { cn } from "@/lib/utils";

/**
 * Telemetry meta bar (docs/12 screen 5): engine label + model from the `meta` event, turn,
 * latency (first token / total) and token usage from `done`, plus the 5/min · 50/day quota
 * meters (docs/03 §2). The daily value comes from X-RateLimit-* headers.
 */
export function ChatTelemetry({ turn, quota }: { turn?: ChatTurn; quota: QuotaState }) {
  const t = useTranslations("chat");
  const meta = turn?.meta;
  const first =
    turn?.firstTokenAt && turn.startedAt ? Math.round(turn.firstTokenAt - turn.startedAt) : null;
  const total =
    turn?.finishedAt && turn.startedAt ? Math.round(turn.finishedAt - turn.startedAt) : null;
  const dailyUsed = quota.dailyRemaining === null ? null : DAILY_LIMIT - quota.dailyRemaining;

  return (
    <div
      data-testid="chat-telemetry"
      aria-label={t("telemetry.label")}
      role="group"
      className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border bg-card/60 px-4 py-2 text-xs text-muted-foreground"
    >
      <span className="flex items-center gap-1.5">
        <Activity className="h-3.5 w-3.5 text-accent" aria-hidden />
        <span className="font-medium text-foreground">{t("telemetry.engine")}</span>
        <span aria-hidden>•</span>
        <span className="font-mono tracking-wide" data-testid="chat-model">
          {meta?.model ?? t("telemetry.modelPending")}
        </span>
      </span>
      {meta && (
        <span className="font-mono tabular-nums" data-testid="chat-turn">
          {t("telemetry.turn", { n: meta.turn })}
        </span>
      )}
      {first !== null && total !== null && (
        <span className="flex items-center gap-1" data-testid="chat-latency">
          <span>{t("telemetry.latency")}:</span>
          <span className="font-mono tabular-nums">
            {t("telemetry.latencyValue", { first, total })}
          </span>
        </span>
      )}
      {turn?.done && (
        <span className="flex items-center gap-1" data-testid="chat-tokens">
          <span>{t("telemetry.tokens")}:</span>
          <span className="font-mono tabular-nums">
            {t("telemetry.tokensValue", { in: turn.done.tokens_in, out: turn.done.tokens_out })}
          </span>
        </span>
      )}
      <span className="ml-auto flex items-center gap-3" aria-label={t("quota.label")} role="group">
        <Gauge className="h-3.5 w-3.5" aria-hidden />
        <QuotaMeter
          testId="quota-minute"
          label={t("quota.minute")}
          used={quota.minuteUsed}
          limit={MINUTE_LIMIT}
          text={t("quota.value", { used: quota.minuteUsed, limit: MINUTE_LIMIT })}
        />
        <QuotaMeter
          testId="quota-daily"
          label={t("quota.daily")}
          used={dailyUsed}
          limit={DAILY_LIMIT}
          text={
            dailyUsed === null
              ? t("quota.unknown", { limit: DAILY_LIMIT })
              : t("quota.value", { used: dailyUsed, limit: DAILY_LIMIT })
          }
        />
      </span>
    </div>
  );
}

function QuotaMeter({
  label,
  used,
  limit,
  text,
  testId,
}: {
  label: string;
  used: number | null;
  limit: number;
  text: string;
  testId: string;
}) {
  const ratio = used === null ? 0 : Math.min(1, used / limit);
  return (
    <span className="flex items-center gap-1.5" data-testid={testId}>
      <span>{label}</span>
      <span
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={limit}
        aria-valuenow={used ?? 0}
        aria-valuetext={text}
        className="relative h-1.5 w-14 overflow-hidden rounded-full bg-secondary"
      >
        <span
          className={cn(
            "absolute inset-y-0 left-0 rounded-full",
            ratio >= 1 ? "bg-destructive" : ratio >= 0.8 ? "bg-warning" : "bg-primary",
          )}
          style={{ width: `${ratio * 100}%` }}
        />
      </span>
      <span className="font-mono tabular-nums text-foreground" data-testid={`${testId}-value`}>
        {text}
      </span>
    </span>
  );
}
