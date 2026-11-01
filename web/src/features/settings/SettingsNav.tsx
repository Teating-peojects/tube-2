"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/utils/format";

const links = [
  { label: "Account", href: "/settings/account" }, { label: "Privacy", href: "/settings/privacy" },
  { label: "Security", href: "/settings/security" }, { label: "Notifications", href: "/settings/notifications" },
  { label: "Playback", href: "/settings/playback" }, { label: "Appearance", href: "/settings/appearance" },
];

export function SettingsNav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Settings" className="-mx-3 flex gap-1 overflow-x-auto px-3 md:mx-0 md:flex-col md:px-0">
      {links.map((l) => {
        const active = pathname === l.href;
        return (
          <Link key={l.href} href={l.href} aria-current={active ? "page" : undefined}
            className={cn("flex h-11 shrink-0 items-center rounded-xl px-4 text-sm transition-colors", active ? "bg-surface font-semibold text-white" : "text-fg-2 hover:bg-card hover:text-white")}>
            {l.label}
          </Link>
        );
      })}
    </nav>
  );
}
