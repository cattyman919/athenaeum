"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { WifiOff } from "lucide-react";

function subscribe(onChange: () => void) {
  window.addEventListener("online", onChange);
  window.addEventListener("offline", onChange);
  return () => {
    window.removeEventListener("online", onChange);
    window.removeEventListener("offline", onChange);
  };
}

/** Best-effort `navigator.onLine` banner (docs/07 §9). Server render assumes online. */
export function OfflineBanner() {
  const t = useTranslations("common.offline");
  const online = React.useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
  if (online) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="offline-banner"
      className="fixed inset-x-0 bottom-0 z-50 flex items-center justify-center gap-2 border-t border-status-out-dot/30 bg-status-out-bg px-4 py-2 text-sm font-medium text-status-out-fg"
    >
      <WifiOff className="h-4 w-4" aria-hidden />
      {t("message")}
    </div>
  );
}
