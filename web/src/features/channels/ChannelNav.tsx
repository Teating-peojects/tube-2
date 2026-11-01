"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/utils/format";

const tabs = [
  { label: "Home", path: "" }, { label: "Videos", path: "/videos" }, { label: "Shorts", path: "/shorts" },
  { label: "Live", path: "/live" }, { label: "Playlists", path: "/playlists" }, { label: "About", path: "/about" },
];

export function ChannelNav({ channelId }: { channelId: string }) {
  const pathname = usePathname();
  const base = `/channel/${channelId}`;
  return (
    <nav aria-label="Channel sections" className="mt-4 flex gap-1 overflow-x-auto border-b border-line">
      {tabs.map((t) => {
        const href = base + t.path;
        const active = pathname === href;
        return (
          <Link key={t.label} href={href} aria-current={active ? "page" : undefined}
            className={cn("relative flex h-11 shrink-0 items-center px-4 text-sm font-semibold transition-colors",
              active ? "text-white after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:rounded-full after:bg-accent" : "text-fg-2 hover:text-white")}>
            {t.label}
          </Link>
        );
      })}
    </nav>
  );
}
