"use client";

import * as React from "react";
import Link from "next/link";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

/**
 * Markdown for AI replies. Raw HTML is never rendered (react-markdown escapes it) and images
 * are dropped; links to our own pages use the router, everything else opens in a new tab
 * with `noopener noreferrer`. Styled with plain Tailwind tokens (no typography plugin).
 */
const components: Components = {
  p: ({ children }) => <p className="my-2 first:mt-0 last:mb-0">{children}</p>,
  h1: ({ children }) => (
    <h3 className="mb-1 mt-4 text-base font-semibold first:mt-0">{children}</h3>
  ),
  h2: ({ children }) => (
    <h3 className="mb-1 mt-4 text-base font-semibold first:mt-0">{children}</h3>
  ),
  h3: ({ children }) => <h4 className="mb-1 mt-3 text-sm font-semibold first:mt-0">{children}</h4>,
  h4: ({ children }) => <h4 className="mb-1 mt-3 text-sm font-semibold first:mt-0">{children}</h4>,
  h5: ({ children }) => <h5 className="mb-1 mt-3 text-sm font-medium first:mt-0">{children}</h5>,
  h6: ({ children }) => <h5 className="mb-1 mt-3 text-sm font-medium first:mt-0">{children}</h5>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  em: ({ children }) => <em className="italic">{children}</em>,
  del: ({ children }) => <del className="text-muted-foreground">{children}</del>,
  ul: ({ children }) => (
    <ul className="my-2 list-disc space-y-1 pl-5 marker:text-muted-foreground">{children}</ul>
  ),
  ol: ({ children }) => (
    <ol className="my-2 list-decimal space-y-1 pl-5 marker:text-muted-foreground">{children}</ol>
  ),
  li: ({ children }) => <li className="pl-0.5 [&>p]:my-0">{children}</li>,
  hr: () => <hr className="my-4 border-border" />,
  blockquote: ({ children }) => (
    <blockquote className="my-2 border-l-2 border-primary/40 pl-3 text-muted-foreground">
      {children}
    </blockquote>
  ),
  a: ({ href, children }) => {
    if (!href) return <>{children}</>;
    const cls = "font-medium text-primary underline underline-offset-2 hover:text-primary/80";
    if (href.startsWith("/") && !href.startsWith("//")) {
      return (
        <Link href={href} className={cls}>
          {children}
        </Link>
      );
    }
    return (
      <a href={href} target="_blank" rel="noopener noreferrer" className={cls}>
        {children}
      </a>
    );
  },
  img: () => null,
  pre: ({ children }) => (
    <pre className="my-2 overflow-x-auto rounded-md border border-border bg-secondary/60 p-3 font-mono text-[12px] leading-relaxed">
      {children}
    </pre>
  ),
  code: ({ className, children }) => (
    // Block code carries a `language-*` class (or newlines); inline code gets a chip.
    <code
      className={cn(
        className?.startsWith("language-") || String(children).includes("\n")
          ? "bg-transparent p-0"
          : "rounded bg-secondary px-1 py-0.5 font-mono text-[0.85em]",
        className,
      )}
    >
      {children}
    </code>
  ),
  table: ({ children }) => (
    <div className="my-2 overflow-x-auto rounded-md border border-border">
      <table className="w-full border-collapse text-left text-[13px]">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="bg-secondary/60">{children}</thead>,
  th: ({ children }) => (
    <th className="border-b border-border px-3 py-1.5 font-medium">{children}</th>
  ),
  td: ({ children }) => (
    <td className="border-b border-border/60 px-3 py-1.5 align-top">{children}</td>
  ),
};

export function Markdown({ children, className }: { children: string; className?: string }) {
  return (
    <div className={cn("min-w-0 break-words", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  );
}
