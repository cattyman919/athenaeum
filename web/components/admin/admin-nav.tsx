"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { LayoutDashboard, Map } from "lucide-react";
import { cn } from "@/lib/utils";

const LINKS = [
  { href: "/admin", label: "Overview", icon: LayoutDashboard },
  { href: "/admin/layout", label: "Stacks manager", icon: Map },
] as const;

/** Sub-navigation shared by /admin and /admin/layout (one Stitch screen, two routes). */
export function AdminNav() {
  const pathname = usePathname();
  return (
    <nav
      aria-label="Admin"
      className="flex gap-1 rounded-lg bg-surface-low p-1"
      data-testid="admin-nav"
    >
      {LINKS.map(({ href, label, icon: Icon }) => {
        const active = pathname === href;
        return (
          <Link
            key={href}
            href={href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
              active && "bg-card text-foreground shadow-sm",
            )}
          >
            <Icon className="h-4 w-4" aria-hidden />
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
