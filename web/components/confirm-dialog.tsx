"use client";

import * as React from "react";
import { Loader2 } from "lucide-react";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";

/**
 * Confirmation step for destructive actions (delete a chat, remove a book, leave a
 * waitlist). Cancel comes first so it gets initial focus; Escape and the overlay cancel.
 * `onConfirm` may be async: the dialog stays open and shows a spinner until it settles,
 * then closes (errors are the caller's to toast).
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  destructive = true,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: React.ReactNode;
  confirmLabel: string;
  onConfirm: () => void | Promise<void>;
  destructive?: boolean;
}) {
  const t = useTranslations("confirm");
  const [busy, setBusy] = React.useState(false);

  async function accept() {
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
      onOpenChange(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent
        role="alertdialog"
        data-testid="confirm-dialog"
        className="top-[30%] max-w-md p-6"
      >
        <DialogTitle className="text-base font-semibold">{title}</DialogTitle>
        <DialogDescription className="mt-2 text-sm text-muted-foreground">
          {description}
        </DialogDescription>
        <div className="mt-6 flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => onOpenChange(false)}
            data-testid="confirm-cancel"
          >
            {t("cancel")}
          </Button>
          <Button
            size="sm"
            variant={destructive ? "destructive" : "default"}
            disabled={busy}
            onClick={() => void accept()}
            data-testid="confirm-accept"
          >
            {busy && (
              <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" aria-hidden />
            )}
            {confirmLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
