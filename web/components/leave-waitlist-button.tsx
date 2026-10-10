"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api-client";

/** Leaves the waitlist for a book and refreshes the server-rendered list. */
export function LeaveWaitlistButton({ bookId }: { bookId: string }) {
  const t = useTranslations("library");
  const router = useRouter();
  const [pending, startTransition] = React.useTransition();
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={pending}
      data-testid="leave-waitlist"
      onClick={() =>
        startTransition(async () => {
          try {
            await api.json(`/api/books/${bookId}/waitlist`, { method: "DELETE" });
            toast.success(t("toast.left"));
            router.refresh();
          } catch {
            toast.error(t("toast.error"));
          }
        })
      }
    >
      {t("holds.leave")}
    </Button>
  );
}
