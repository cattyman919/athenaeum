import { useTranslations } from "next-intl";
import { Badge } from "@/components/ui/badge";
import { enumLabel } from "@/lib/enum-labels";

const VARIANT: Record<string, "available" | "out" | "unavailable"> = {
  available: "available",
  checked_out: "out",
  reserved_waitlist: "out",
  not_in_building: "unavailable",
  missing_lost: "unavailable",
};

const DOT: Record<"available" | "out" | "unavailable", string> = {
  available: "bg-status-available-dot",
  out: "bg-status-out-dot",
  unavailable: "bg-status-na-dot",
};

/** Availability beacon: status chip recipes from docs/12 §3, labels via enum maps (docs/14 §6). */
export function AvailabilityBadge({ state, className }: { state: string; className?: string }) {
  const t = useTranslations("enums");
  const variant = VARIANT[state] ?? "unavailable";
  return (
    <Badge
      variant={variant}
      className={className}
      data-state={state}
      data-testid="availability-badge"
    >
      <span className={`h-1.5 w-1.5 rounded-full ${DOT[variant]}`} aria-hidden />
      {enumLabel(t, "availability", state)}
    </Badge>
  );
}
