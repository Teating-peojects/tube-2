"use client";
import { Bell, ChatCircle, List, MagnifyingGlass } from "@phosphor-icons/react";
import Link from "next/link";
import { Suspense } from "react";
import { Logo } from "@/components/ui/Logo";
import { useSession } from "@/features/auth/useSession";
import { useUnreadCount } from "@/features/notifications/useUnread";
import { SearchBox } from "@/features/search/SearchBox";
import { useUIStore } from "@/store/ui";

const ib = "relative grid size-11 place-items-center rounded-full text-white transition-colors hover:bg-surface";

export function Header() {
  const toggle = useUIStore((s) => s.toggleSidebar);
  const { user, status } = useSession();
  const unread = useUnreadCount().data ?? 0;

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center gap-3 border-b border-line bg-bg/90 px-3 backdrop-blur md:px-5">
      <button type="button" onClick={toggle} aria-label="Toggle sidebar" className={`${ib} hidden lg:grid`}>
        <List size={22} />
      </button>
      <div className="lg:min-w-40"><Logo /></div>
      <div className="mx-auto hidden max-w-xl flex-1 md:block">
        <Suspense fallback={<div className="skeleton h-11 rounded-full" aria-hidden="true" />}>
          <SearchBox />
        </Suspense>
      </div>
      <div className="ml-auto flex items-center gap-1 md:ml-0">
        <Link href="/search" aria-label="Search" className={`${ib} md:hidden`}><MagnifyingGlass size={22} /></Link>
        <Link href="/upload" className="mr-1 hidden h-10 items-center rounded-full bg-accent px-5 text-sm font-semibold transition-colors hover:bg-accent-hover sm:flex">Upload</Link>
        {status === "authenticated" && user ? (
          <>
            <Link href="/notifications" aria-label={unread > 0 ? `Notifications, ${unread} unread` : "Notifications"} className={ib}>
              <Bell size={22} />
              {unread > 0 && <span aria-hidden="true" className="absolute right-2.5 top-2.5 size-2.5 rounded-full border-2 border-bg bg-accent" />}
            </Link>
            <Link href="/messages" aria-label="Messages" className={`${ib} hidden sm:grid`}><ChatCircle size={22} /></Link>
            <Link href="/profile" aria-label={`Your account, ${user.displayName}`} className={ib}>
              <span className="grid size-8 place-items-center rounded-full border border-line bg-surface text-xs font-bold">{user.displayName.charAt(0).toUpperCase()}</span>
            </Link>
          </>
        ) : status === "anonymous" || status === "error" ? (
          <Link href="/login" className="inline-flex h-10 items-center rounded-full border border-line bg-surface px-4 text-sm font-semibold transition-colors hover:bg-line">Log in</Link>
        ) : (
          <span className="size-10 rounded-full skeleton" aria-hidden="true" />
        )}
      </div>
    </header>
  );
}
