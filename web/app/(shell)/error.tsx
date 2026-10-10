"use client";

import { RouteError } from "@/components/route-error";

// Shell-wide error boundary (docs/07 §9: 5xx → inline panel + retry). Route-level
// `loading.tsx` stays off on purpose — it froze soft navigation in Next 15.5 prod.
export default function ShellError(props: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return <RouteError {...props} />;
}
