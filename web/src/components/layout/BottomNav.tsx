"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { NavIcon } from "@/components/ui/NavIcon";
import { bottomNav } from "@/config/nav";
import { cn } from "@/utils/format";

export function BottomNav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Primary" className="fixed inset-x-0 bottom-0 z-30 grid h-[68px] grid-cols-5 items-center border-t border-line bg-card/90 pb-[env(safe-area-inset-bottom)] backdrop-blur lg:hidden">
      {bottomNav.map((i) => {
        const active = i.href === "/" ? pathname === "/" : pathname.startsWith(i.href);
        if (i.label === "Upload")
          return (
            <Link key={i.href} href={i.href} aria-label="Upload" className="mx-auto grid size-12 place-items-center rounded-full bg-accent text-white shadow-[0_6px_18px_rgba(255,0,60,.35)]">
              <NavIcon name={i.icon} size={24} />
            </Link>
          );
        return (
          <Link key={i.href} href={i.href} aria-current={active ? "page" : undefined}
            className={cn("flex min-h-11 flex-col items-center justify-center gap-0.5 text-[10px]", active ? "text-white" : "text-fg-2")}>
            <span className={active ? "text-accent" : ""}><NavIcon name={i.icon} size={22} weight={active ? "fill" : "regular"} /></span>
            {i.label}
          </Link>
        );
      })}
    </nav>
  );
}
