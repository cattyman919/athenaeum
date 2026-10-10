import { getTranslations } from "next-intl/server";
import { CircleAlert, CircleCheck, CircleSlash, Clock } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { BookDetail } from "@/lib/types";

const VARIANT = {
  available: "success",
  checked_out: "warning",
  reserved_waitlist: "warning",
  not_in_building: "destructive",
  missing_lost: "destructive",
} as const;

/** Tri-state availability banner (docs/12 screen 3). `children` carries the waitlist CTA. */
export async function AvailabilityBanner({
  availability,
  children,
}: {
  availability: BookDetail["availability"];
  children?: React.ReactNode;
}) {
  const t = await getTranslations("book.banner");
  const state = availability.state as keyof typeof VARIANT;
  const variant = VARIANT[state] ?? "destructive";
  const Icon = {
    success: CircleCheck,
    warning: Clock,
    destructive: availability.total_copies === 0 ? CircleSlash : CircleAlert,
  }[variant];

  const copy: Record<string, [string, string]> = {
    available: [
      t("available", { count: availability.available_copies }),
      t("availableHint", {
        available: availability.available_copies,
        total: availability.total_copies,
      }),
    ],
    checked_out: [t("checkedOut"), t("checkedOutHint")],
    reserved_waitlist: [t("reserved"), t("reservedHint")],
    not_in_building: [t("notInBuilding"), t("notInBuildingHint")],
    missing_lost: [t("missing"), t("missingHint")],
  };
  const [title, hint] = copy[state] ?? copy.not_in_building;

  return (
    <Alert variant={variant} data-testid="availability-banner" data-state={availability.state}>
      <Icon className="mt-0.5 h-5 w-5 shrink-0" aria-hidden />
      <div className="flex-1">
        <AlertTitle>{title}</AlertTitle>
        <AlertDescription>{hint}</AlertDescription>
        {children && <div className="mt-3">{children}</div>}
      </div>
    </Alert>
  );
}
