"use client";

// BarcodeScannerDialog (docs/11 §6–§7, Stitch artifact .artifacts/stitch/barscanner_ISBN):
// one Radix dialog with a Patron Lookup / Staff Intake mode switcher. Staff Intake is
// admin-only and stays English (docs/14 §1); Patron Lookup is localized (`scanner.*`).

import * as React from "react";
import { useTranslations } from "next-intl";
import { Inbox, ScanLine, Search, X } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PatronLookupPanel } from "./patron-lookup-panel";
import { StaffIntakePanel } from "./staff-intake-panel";

export type ScannerMode = "patron" | "staff";

export interface BarcodeScannerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Initial tab; the staff tab is hidden (and ignored) for non-admins. */
  mode?: ScannerMode;
  isAdmin: boolean;
}

export function BarcodeScannerDialog({
  open,
  onOpenChange,
  mode = "patron",
  isAdmin,
}: BarcodeScannerDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="top-[3%] max-h-[94vh] max-w-5xl overflow-y-auto rounded-xl border-0 p-0"
        data-testid="scanner-dialog"
        aria-describedby="scanner-dialog-desc"
      >
        {open && (
          <ScannerBody
            initialMode={isAdmin ? mode : "patron"}
            isAdmin={isAdmin}
            onClose={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ScannerBody({
  initialMode,
  isAdmin,
  onClose,
}: {
  initialMode: ScannerMode;
  isAdmin: boolean;
  onClose: () => void;
}) {
  const t = useTranslations("scanner");
  const [tab, setTab] = React.useState<ScannerMode>(initialMode);
  const [handoverIsbn, setHandoverIsbn] = React.useState<string | undefined>(undefined);

  return (
    <Tabs value={tab} onValueChange={(v) => setTab(v as ScannerMode)} data-mode={tab}>
      <div className="flex flex-wrap items-center justify-between gap-4 px-6 py-4">
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary shadow-sm">
            <ScanLine className="h-6 w-6" aria-hidden />
          </span>
          <div className="min-w-0">
            <DialogTitle className="truncate text-lg font-semibold tracking-tight">
              {t("title")}
            </DialogTitle>
            <DialogDescription
              id="scanner-dialog-desc"
              className="truncate text-sm text-on-surface-variant"
            >
              {t("subtitle")} <span className="font-mono text-xs text-primary">ATH-XXXXXX-CYY</span>
            </DialogDescription>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {isAdmin && (
            <TabsList className="w-auto shadow-inner" aria-label={t("tabs.label")}>
              <TabsTrigger value="patron" data-testid="scanner-tab-patron" className="gap-1.5">
                <Search className="h-4 w-4" aria-hidden />
                {t("tabs.patron")}
              </TabsTrigger>
              <TabsTrigger value="staff" data-testid="scanner-tab-staff" className="gap-1.5">
                <Inbox className="h-4 w-4" aria-hidden />
                {t("tabs.staff")}
                <span className="rounded bg-accent/20 px-1 font-mono text-[10px] font-bold text-on-surface">
                  {t("tabs.staffBadge")}
                </span>
              </TabsTrigger>
            </TabsList>
          )}
          <button
            type="button"
            onClick={onClose}
            aria-label={t("close")}
            className="flex h-8 w-8 items-center justify-center rounded text-on-surface-variant transition-colors hover:bg-surface-container hover:text-on-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <X className="h-5 w-5" aria-hidden />
          </button>
        </div>
      </div>

      <TabsContent value="patron" className="mt-0">
        <PatronLookupPanel
          active={tab === "patron"}
          onNavigate={onClose}
          onStaffIntake={
            isAdmin
              ? (isbn) => {
                  setHandoverIsbn(isbn);
                  setTab("staff");
                }
              : undefined
          }
        />
      </TabsContent>
      {isAdmin && (
        <TabsContent value="staff" className="mt-0">
          <StaffIntakePanel initialIsbn={handoverIsbn} onClose={onClose} />
        </TabsContent>
      )}
    </Tabs>
  );
}
