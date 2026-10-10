"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useFormatter, useTranslations } from "next-intl";
import { toast } from "sonner";
import { BellRing } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api, ApiError } from "@/lib/api-client";
import { apiErrorText } from "@/lib/api-errors";

export interface HoldState {
  status: "waiting" | "ready";
  position: number;
  expires_at?: string;
}

/**
 * Availability-banner waitlist CTA (docs/07 §7 screen 3 + §9, docs/03 §6.3): shown when the
 * book has zero available copies. Join → position; leave → back to the join button.
 */
export function WaitlistCta({ bookId, hold: initial }: { bookId: string; hold: HoldState | null }) {
  const t = useTranslations("waitlist");
  const tCommon = useTranslations("common");
  const format = useFormatter();
  const router = useRouter();
  const [hold, setHold] = React.useState<HoldState | null>(initial);
  const [pending, startTransition] = React.useTransition();

  function join() {
    startTransition(async () => {
      try {
        const w = await api.joinWaitlist(bookId);
        setHold({ status: w.status, position: w.position });
        toast.success(t("joined"));
        router.refresh();
      } catch (err) {
        toast.error(
          err instanceof ApiError && err.status === 409
            ? err.message
            : apiErrorText(tCommon, err, t("error")),
        );
      }
    });
  }

  function leave() {
    startTransition(async () => {
      try {
        await api.leaveWaitlist(bookId);
        setHold(null);
        toast.success(t("left"));
        router.refresh();
      } catch (err) {
        toast.error(apiErrorText(tCommon, err, t("error")));
      }
    });
  }

  if (!hold) {
    return (
      <div className="flex flex-wrap items-center gap-3" data-testid="waitlist-cta">
        <Button size="sm" disabled={pending} onClick={join} data-testid="waitlist-join">
          <BellRing className="h-4 w-4" aria-hidden />
          {t("join")}
        </Button>
        <span className="text-xs">{t("hint")}</span>
      </div>
    );
  }

  return (
    <div
      className="flex flex-wrap items-center gap-3"
      data-testid="waitlist-cta"
      data-hold-status={hold.status}
    >
      <span className="text-sm font-medium" data-testid="waitlist-position">
        {hold.status === "ready" && hold.expires_at
          ? t("ready", {
              date: format.dateTime(new Date(hold.expires_at), { dateStyle: "medium" }),
            })
          : t("position", { position: hold.position })}
      </span>
      <Button
        size="sm"
        variant="outline"
        disabled={pending}
        onClick={leave}
        data-testid="waitlist-leave"
      >
        {t("leave")}
      </Button>
    </div>
  );
}
