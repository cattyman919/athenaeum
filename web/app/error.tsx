"use client";

import { RouteError } from "@/components/route-error";

// Error boundary for routes outside the app shell (e.g. /login) — docs/07 §9.
export default function RootError(props: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <main className="px-6">
      <RouteError {...props} />
    </main>
  );
}
