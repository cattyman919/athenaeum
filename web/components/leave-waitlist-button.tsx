"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api-client";
import { apiErrorText } from "@/lib/api-errors";

/** Leaves the waitlist for a book and refreshes the server-rendered list. */
export function LeaveWaitlistButton({ bookId }: { bookId: string }) {
  const t = useTranslations("library");
  const tCommon = useTranslations("common");
  const router = useRouter();
  const [pending, startTransition] = React.useTransition();
  const tConfirm = useTranslations("confirm");
  const [confirm, setConfirm] = React.useState(false);
  const leave = () =>
    startTransition(async () => {
      try {
        await api.json(`/api/books/${bookId}/waitlist`, { method: "DELETE" });
        toast.success(t("toast.left"));
        router.refresh();
      } catch (err) {
        toast.error(apiErrorText(tCommon, err, t("toast.error")));
      }
    });
  return (
    <>
      <Button
        variant="outline"
        size="sm"
        disabled={pending}
        data-testid="leave-waitlist"
        onClick={() => setConfirm(true)}
      >
        {t("holds.leave")}
      </Button>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={tConfirm("waitlistLeave.title")}
        description={tConfirm("waitlistLeave.description")}
        confirmLabel={tConfirm("waitlistLeave.action")}
        onConfirm={leave}
      />
    </>
  );
}
