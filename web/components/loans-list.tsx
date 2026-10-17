"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useFormatter, useTranslations } from "next-intl";
import { toast } from "sonner";
import { BookCover } from "@/components/book-cover";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api-client";
import { apiErrorText } from "@/lib/api-errors";
import { isDueSoon } from "@/lib/loans";
import type { Loan } from "@/lib/types";

export function LoansList({ loans }: { loans: Loan[] }) {
  return (
    <ul className="flex flex-col gap-3" data-testid="loans-list">
      {loans.map((loan) => (
        <LoanRow key={loan.id} loan={loan} />
      ))}
    </ul>
  );
}

function LoanRow({ loan }: { loan: Loan }) {
  const t = useTranslations("library");
  const tCommon = useTranslations("common");
  const tConfirm = useTranslations("confirm");
  const format = useFormatter();
  const router = useRouter();
  const [pending, startTransition] = React.useTransition();
  const [confirmReturn, setConfirmReturn] = React.useState(false);

  const renew = () =>
    startTransition(async () => {
      try {
        await api.renewCopy(loan.copy_id);
        toast.success(t("loans.renewed"));
        router.refresh();
      } catch (err) {
        toast.error(apiErrorText(tCommon, err, t("toast.error")));
      }
    });

  const doReturn = async () => {
    try {
      await api.returnCopy(loan.copy_id);
      toast.success(t("loans.returnedToast"));
      router.refresh();
    } catch (err) {
      toast.error(apiErrorText(tCommon, err, t("toast.error")));
    }
  };

  const dueSoon = isDueSoon(loan);

  return (
    <li
      data-testid="loan-row"
      className="flex items-center gap-4 rounded-lg border border-border bg-card p-4"
    >
      <div className="w-12 shrink-0">
        <BookCover src={loan.book.cover_url} alt={loan.book.title} title={loan.book.title} />
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <Link href={`/books/${loan.book.id}`} className="truncate font-semibold hover:text-primary">
          {loan.book.title}
        </Link>
        <p className="text-xs text-muted-foreground">{loan.book.authors.join(", ")}</p>
        <p className="mono-label text-muted-foreground">
          {t("loans.barcode")}: {loan.barcode}
        </p>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>
            {t("loans.borrowed", {
              date: format.dateTime(new Date(loan.borrowed_at), { dateStyle: "medium" }),
            })}
          </span>
          {loan.kind === "active" ? (
            <>
              <span data-testid="loan-due">
                {t("loans.due", {
                  date: format.dateTime(new Date(loan.due_at), { dateStyle: "medium" }),
                })}
              </span>
              {loan.overdue && (
                <Badge variant="out" data-testid="loan-overdue">
                  {t("loans.overdue")}
                </Badge>
              )}
              {dueSoon && <span className="text-muted-foreground">{t("loans.dueSoon")}</span>}
            </>
          ) : (
            <span>
              {t("loans.returned", {
                date: format.dateTime(new Date(loan.returned_at!), { dateStyle: "medium" }),
              })}
            </span>
          )}
        </div>
      </div>
      {loan.kind === "active" && (
        <div className="flex shrink-0 flex-col gap-2">
          {loan.renewable && (
            <Button
              variant="outline"
              size="sm"
              disabled={pending}
              data-testid="loan-renew"
              onClick={renew}
            >
              {t("loans.renew")}
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            disabled={pending}
            data-testid="loan-return"
            onClick={() => setConfirmReturn(true)}
          >
            {tConfirm("loanReturn.action")}
          </Button>
        </div>
      )}
      <ConfirmDialog
        open={confirmReturn}
        onOpenChange={setConfirmReturn}
        title={tConfirm("loanReturn.title")}
        description={tConfirm("loanReturn.description")}
        confirmLabel={tConfirm("loanReturn.action")}
        onConfirm={doReturn}
      />
    </li>
  );
}
