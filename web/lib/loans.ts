// Loan display helpers (docs/03 §6.5-6.7; 14-day term, renew once).

import type { Loan } from "./types";

const DAY_MS = 86_400_000;

/** Whole days from now until `dueAt` (negative when past due). */
export function daysUntil(dueAt: string, nowMs = Date.now()): number {
  return Math.ceil((new Date(dueAt).getTime() - nowMs) / DAY_MS);
}

/** Due within 3 days and not overdue. */
export function isDueSoon(loan: Pick<Loan, "due_at" | "overdue" | "kind">, nowMs = Date.now()) {
  if (loan.kind !== "active" || loan.overdue) return false;
  return daysUntil(loan.due_at, nowMs) <= 3;
}

/** Active loans first (earliest due first), then past loans newest-returned first. */
export function sortLoans(loans: Loan[]): Loan[] {
  const active = loans
    .filter((l) => l.kind === "active")
    .sort((a, b) => new Date(a.due_at).getTime() - new Date(b.due_at).getTime());
  const past = loans
    .filter((l) => l.kind === "past")
    .sort(
      (a, b) => new Date(b.returned_at ?? 0).getTime() - new Date(a.returned_at ?? 0).getTime(),
    );
  return [...active, ...past];
}
