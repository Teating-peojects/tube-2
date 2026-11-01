"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { NavIcon } from "@/components/ui/NavIcon";
import { creatorNav, mainNav, type NavItem } from "@/config/nav";
import { useUIStore } from "@/store/ui";
import { cn } from "@/utils/format";

function Item({ item, collapsed, active }: { item: NavItem; collapsed: boolean; active: boolean }) {
  return (
    <Link href={item.href} aria-current={active ? "page" : undefined} title={collapsed ? item.label : undefined}
      className={cn("flex h-11 items-center gap-4 rounded-xl px-3 text-sm transition-colors",
        active ? "bg-surface text-white shadow-[inset_3px_0_0_var(--color-accent)]" : "text-fg-2 hover:bg-card hover:text-white",
        collapsed && "justify-center px-0")}>
      <span className={active ? "text-accent" : ""}><NavIcon name={item.icon} weight={active ? "fill" : "regular"} /></span>
      <span className={collapsed ? "sr-only" : ""}>{item.label}</span>
    </Link>
  );
}

export function Sidebar() {
  const pathname = usePathname();
  const collapsed = useUIStore((s) => s.sidebarCollapsed);
  const isActive = (href: string) => (href === "/" ? pathname === "/" : pathname.startsWith(href));

  return (
    <nav aria-label="Main" className={cn("sticky top-16 hidden h-[calc(100dvh-4rem)] shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-line p-2.5 transition-[width] duration-200 lg:flex", collapsed ? "w-[72px]" : "w-56")}>
      {mainNav.map((i) => <Item key={i.href} item={i} collapsed={collapsed} active={isActive(i.href)} />)}
      <p className={cn("mx-3 mb-1.5 mt-5 text-[11px] font-semibold tracking-[0.12em] text-fg-3", collapsed && "sr-only")}>CREATOR</p>
      {creatorNav.map((i) => <Item key={i.href} item={i} collapsed={collapsed} active={isActive(i.href)} />)}
    </nav>
  );
}
