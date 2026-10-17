"use client";

// Staff Intake tab (docs/11 §2c, §7 "Staff module"): the existing Add-Book flow — fetch-isbn
// prefill → confirm → POST /api/admin/books with auto-assigned ATH copy tags. Admin-only →
// English (docs/14 §1). RFID encode / label print / Edit-MARC stay out of v1 (docs/11 §11).

import { Tag } from "lucide-react";
import { AddBookFlow } from "@/components/isbn-scanner";

export function StaffIntakePanel({
  initialIsbn,
  onClose,
}: {
  initialIsbn?: string;
  onClose: () => void;
}) {
  return (
    <div className="space-y-4 bg-surface-low p-6" data-testid="staff-intake-panel">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-primary/5 p-3">
        <p className="text-sm text-on-surface-variant">
          Scan or type an ISBN to fetch the book, check the cover, then pick its section and shelf.
        </p>
        <span className="flex items-center gap-1.5 rounded bg-card px-2 py-1 font-mono text-xs text-primary shadow-sm">
          <Tag className="h-3.5 w-3.5" aria-hidden />
          Copy tags auto-assigned: ATH-XXXXXX-CY
        </span>
      </div>
      <div className="rounded-lg bg-card p-4 shadow-sm">
        <AddBookFlow key={initialIsbn ?? "blank"} initialIsbn={initialIsbn} onClose={onClose} />
      </div>
    </div>
  );
}
