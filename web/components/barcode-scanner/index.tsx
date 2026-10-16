"use client";

// Universal barcode scanner entry points (docs/11 §5–§6, docs/07 §Scanner dialog). The dialog
// itself is browser-only (getUserMedia + WASM) and loaded with `ssr:false`; one ScannerHost in
// the app shell owns it, and every trigger (nav Scan, /library, /admin) opens it via an event.

import * as React from "react";
import dynamic from "next/dynamic";
import { useTranslations } from "next-intl";
import { PackagePlus, ScanLine, Undo2 } from "lucide-react";
import { Button, type ButtonProps } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useAuth } from "@/providers/auth-provider";
import type { ScannerMode } from "./scanner-dialog";

export type { ScannerMode } from "./scanner-dialog";

export const BarcodeScannerDialog = dynamic(
  () => import("./scanner-dialog").then((m) => m.BarcodeScannerDialog),
  {
    ssr: false,
    loading: () => (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
        <Skeleton className="h-64 w-full max-w-5xl" />
      </div>
    ),
  },
);

export const OPEN_SCANNER_EVENT = "athenaeum:open-scanner";

/** Opens the shared scanner dialog from anywhere on the client. */
export function openScanner(mode: ScannerMode = "patron") {
  window.dispatchEvent(
    new CustomEvent<{ mode: ScannerMode }>(OPEN_SCANNER_EVENT, { detail: { mode } }),
  );
}

/** Mounted once in the app shell; lazily loads the dialog on first open. */
export function ScannerHost() {
  const { isAdmin } = useAuth();
  const [state, setState] = React.useState<{ open: boolean; mode: ScannerMode; loaded: boolean }>({
    open: false,
    mode: "patron",
    loaded: false,
  });

  React.useEffect(() => {
    const onOpen = (e: Event) => {
      const mode = (e as CustomEvent<{ mode?: ScannerMode }>).detail?.mode ?? "patron";
      setState({ open: true, mode, loaded: true });
    };
    window.addEventListener(OPEN_SCANNER_EVENT, onOpen);
    return () => window.removeEventListener(OPEN_SCANNER_EVENT, onOpen);
  }, []);

  if (!state.loaded) return null;
  return (
    <BarcodeScannerDialog
      open={state.open}
      onOpenChange={(open) => setState((s) => ({ ...s, open }))}
      mode={state.mode}
      isAdmin={isAdmin}
    />
  );
}

/** Top-nav Scan button (all users, Patron Lookup — docs/12 §6). */
export function ScanNavButton() {
  const t = useTranslations("scanner");
  return (
    <Button
      variant="ghost"
      size="sm"
      data-testid="open-scanner"
      aria-label={t("openLabel")}
      onClick={() => openScanner("patron")}
    >
      <ScanLine className="h-4 w-4" aria-hidden />
      <span className="hidden sm:inline">{t("open")}</span>
    </Button>
  );
}

/** `/library` "Scan to return" (Patron Lookup — docs/07 screen 4). */
export function ScanToReturnButton(props: Omit<ButtonProps, "onClick">) {
  const t = useTranslations("scanner");
  return (
    <Button
      variant="outline"
      size="sm"
      data-testid="library-scan-return"
      onClick={() => openScanner("patron")}
      {...props}
    >
      <Undo2 className="h-4 w-4" aria-hidden />
      {t("libraryReturn")}
    </Button>
  );
}

/** `/admin` toolbar entry opening Staff Intake (admin-only → English, docs/14 §1). */
export function StaffIntakeButton(props: Omit<ButtonProps, "onClick">) {
  return (
    <Button
      variant="outline"
      data-testid="admin-open-intake"
      onClick={() => openScanner("staff")}
      {...props}
    >
      <PackagePlus className="h-4 w-4" aria-hidden />
      Scan intake
    </Button>
  );
}
