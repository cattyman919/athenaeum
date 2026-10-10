"use client";

import * as React from "react";
import { BookOpen } from "lucide-react";
import { cn } from "@/lib/utils";

/** Strict 2:3 jacket with a 4px radius and an inset hairline (docs/12 §5); falls back to a
 *  typographic placeholder when the cover URL is missing or fails to load (offline mode). */
export function BookCover({
  src,
  alt,
  title,
  accent,
  className,
}: {
  src: string | null;
  alt: string;
  title: string;
  accent?: string;
  className?: string;
}) {
  const [failed, setFailed] = React.useState(false);
  const showImage = src && !failed;
  return (
    <div
      className={cn(
        "relative aspect-[2/3] w-full overflow-hidden rounded-[4px] bg-surface-container ring-1 ring-inset ring-black/5",
        className,
      )}
      style={accent ? { borderLeft: `3px solid ${accent}` } : undefined}
    >
      {showImage ? (
        // eslint-disable-next-line @next/next/no-img-element -- remote Open Library covers
        <img
          src={src}
          alt={alt}
          loading="lazy"
          onError={() => setFailed(true)}
          className="h-full w-full object-cover"
        />
      ) : (
        <div
          role="img"
          aria-label={alt}
          className="flex h-full w-full flex-col items-center justify-center gap-2 p-3 text-center"
        >
          <BookOpen className="h-6 w-6 text-outline" aria-hidden />
          <span className="line-clamp-4 text-xs font-medium text-on-surface-variant">{title}</span>
        </div>
      )}
    </div>
  );
}
