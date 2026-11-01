"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/utils/format";

const tabs = [
  { label: "Overview", href: "/studio" }, { label: "Videos", href: "/studio/videos" },
  { label: "Analytics", href: "/studio/analytics" }, { label: "Comments", href: "/studio/comments" },
];

export function StudioNav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Studio sections" className="flex gap-1 overflow-x-auto border-b border-line">
      {tabs.map((t) => {
        const active = t.href === "/studio" ? pathname === t.href : pathname.startsWith(t.href);
        return (
          <Link key={t.href} href={t.href} aria-current={active ? "page" : undefined}
            className={cn("relative flex h-11 shrink-0 items-center px-4 text-sm font-semibold transition-colors",
              active ? "text-white after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:rounded-full after:bg-accent" : "text-fg-2 hover:text-white")}>
            {t.label}
          </Link>
        );
      })}
    </nav>
  );
}
