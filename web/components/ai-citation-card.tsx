"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";
import { ArrowUpRight, MapPin } from "lucide-react";
import { SECTIONS } from "@/lib/catalog-query";
import { parseShelfPath } from "@/lib/chat-stream";
import type { ChatCitation } from "@/lib/types";
import { sectionLabel, shelfLine } from "@/lib/wayfinding";

/**
 * Citation card for a `citations` SSE entry (docs/06 §6, docs/12 §9): title deep link,
 * localized shelf line from the path token (docs/14 §6.1 — codes stay untranslated),
 * section hairline color, snippet and retrieval score.
 */
export function AICitationCard({ citation }: { citation: ChatCitation }) {
  const t = useTranslations("chat.citations");
  const tWay = useTranslations("wayfinding");
  const loc = parseShelfPath(citation.path);
  const section = loc ? SECTIONS.find((s) => s.code === loc.section) : undefined;
  const pct = Math.round(Math.max(0, Math.min(1, citation.score)) * 100);

  return (
    <Link
      href={`/books/${citation.book_id}`}
      data-testid="citation-card"
      aria-label={t("open", { title: citation.title })}
      className="group relative flex flex-col gap-1.5 overflow-hidden rounded-lg border border-border bg-card p-3 pl-4 shadow-sm transition-all duration-150 hover:border-primary/40 hover:shadow-md motion-reduce:transition-none"
    >
      <span
        aria-hidden
        className="absolute inset-y-0 left-0 w-1"
        style={{ backgroundColor: section?.color ?? "hsl(var(--border))" }}
      />
      <span className="flex items-start justify-between gap-2">
        <span className="text-sm font-semibold leading-snug" data-testid="citation-title">
          {citation.title}
        </span>
        <ArrowUpRight
          className="h-4 w-4 shrink-0 text-muted-foreground group-hover:text-primary"
          aria-hidden
        />
      </span>
      {loc ? (
        <span className="flex flex-col gap-0.5 text-xs text-muted-foreground">
          <span className="flex items-center gap-1" data-testid="citation-shelf">
            <MapPin className="h-3.5 w-3.5 text-accent" aria-hidden />
            {shelfLine(tWay, { floor_number: loc.floor, shelf_code: loc.shelf })}
            {section && <span> · {sectionLabel(tWay, section.code, section.name)}</span>}
          </span>
          <span
            className="font-mono text-[11px] uppercase tracking-wider text-foreground/80"
            title={t("pathLabel")}
            data-testid="citation-path"
          >
            {citation.path}
          </span>
        </span>
      ) : citation.path ? (
        <span
          className="font-mono text-[11px] uppercase tracking-wider"
          data-testid="citation-path"
        >
          {citation.path}
        </span>
      ) : (
        <span className="text-xs text-muted-foreground">{t("noShelf")}</span>
      )}
      {citation.snippet && (
        <span className="line-clamp-2 text-xs leading-relaxed text-muted-foreground">
          {citation.snippet}
        </span>
      )}
      {citation.score > 0 && (
        <span className="font-mono text-[11px] tabular-nums text-primary">
          {t("match", { pct })}
        </span>
      )}
    </Link>
  );
}
