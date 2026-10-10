"use client";

import * as React from "react";

/** Flags the document once React has hydrated; Playwright waits on it (docs/09 §8.1). */
export function HydrationMarker() {
  React.useEffect(() => {
    document.documentElement.dataset.hydrated = "true";
  }, []);
  return null;
}
