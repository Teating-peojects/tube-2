"use client";
import { ArrowBendUpLeft, Bell, CheckCircle, ChatCircle, Info, UserPlus, VideoCamera, WarningCircle, type Icon } from "@phosphor-icons/react";
import { useInfiniteQuery, useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { Tabs } from "@/components/ui/Tabs";
import { safeNext } from "@/lib/auth";
import { getNotifications, markNotificationsRead } from "@/services/notifications";
import type { AppNotification, NotificationType, Paginated } from "@/types";
import { cn, timeAgo } from "@/utils/format";
import { unreadKey, useUnreadCount } from "./useUnread";

const icons: Record<NotificationType, Icon> = {
  new_subscriber: UserPlus, new_comment: ChatCircle, comment_reply: ArrowBendUpLeft, creator_upload: VideoCamera,
  system: Info, processing_completed: CheckCircle, processing_failed: WarningCircle,
};
type Filter = "all" | "unread";
type Data = InfiniteData<Paginated<AppNotification>>;
const tabs: { id: Filter; label: string }[] = [{ id: "all", label: "All" }, { id: "unread", label: "Unread" }];

export function NotificationsView() {
  const qc = useQueryClient();
  const [filter, setFilter] = useState<Filter>("all");
  const unread = useUnreadCount();

  const q = useInfiniteQuery({
    queryKey: ["notifications", filter],
    queryFn: ({ pageParam }) => getNotifications({ cursor: pageParam, unreadOnly: filter === "unread" }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];

  const markRead = useMutation({
    mutationFn: markNotificationsRead,
    onMutate: async (p: { ids?: string[]; all?: boolean }) => {
      await qc.cancelQueries({ queryKey: ["notifications"] });
      const prevLists = qc.getQueriesData<Data>({ queryKey: ["notifications"] });
      const prevCount = qc.getQueryData<number>(unreadKey);
      qc.setQueriesData<Data>({ queryKey: ["notifications"] }, (old) => old && {
        ...old, pages: old.pages.map((pg) => ({ ...pg, items: pg.items.map((n) => (p.all || p.ids?.includes(n.id) ? { ...n, read: true } : n)) })),
      });
      const newlyRead = p.all ? (prevCount ?? 0) : items.filter((n) => !n.read && p.ids?.includes(n.id)).length;
      qc.setQueryData<number>(unreadKey, Math.max(0, (prevCount ?? 0) - newlyRead));
      return { prevLists, prevCount };
    },
    onError: (_e, _p, ctx) => { // rollback
      ctx?.prevLists.forEach(([k, d]) => qc.setQueryData(k, d));
      if (ctx?.prevCount !== undefined) qc.setQueryData(unreadKey, ctx.prevCount);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: unreadKey }),
  });

  return (
    <div className="mx-auto max-w-3xl">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-extrabold">Notifications</h1>
        <Button variant="secondary" className="h-10" disabled={!unread.data} onClick={() => markRead.mutate({ all: true })}>Mark all as read</Button>
      </div>
      <Tabs tabs={tabs} value={filter} onChange={setFilter} label="Notification filter" idPrefix="notif" />
      <div role="tabpanel" id="notif-panel" aria-labelledby={`notif-tab-${filter}`} className="pt-4">
        {q.isPending && <ul className="space-y-2" aria-hidden="true">{Array.from({ length: 6 }, (_, i) => <li key={i} className="skeleton h-20 rounded-card" />)}</ul>}
        {q.isError && (
          <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
            <p className="font-semibold">Couldn’t load notifications</p>
            <Button onClick={() => q.refetch()}>Retry</Button>
          </div>
        )}
        {q.isSuccess && items.length === 0 && (
          <EmptyState icon={<Bell size={28} />} title={filter === "unread" ? "You’re all caught up" : "No notifications"} text={filter === "unread" ? "No unread notifications." : "Updates about your channel and subscriptions will show up here."} />
        )}
        {items.length > 0 && (
          <ul className="space-y-2">
            {items.map((n) => {
              const I = icons[n.type];
              return (
                <li key={n.id}>
                  <Link href={safeNext(n.href, "/notifications")} onClick={() => { if (!n.read) markRead.mutate({ ids: [n.id] }); }}
                    className={cn("flex items-start gap-4 rounded-card border p-4 transition-colors hover:bg-surface", n.read ? "border-line bg-card" : "border-line bg-surface")}>
                    <span aria-hidden="true" className={cn("grid size-10 shrink-0 place-items-center rounded-full bg-bg", n.type === "processing_failed" ? "text-accent" : "text-fg-2")}><I size={20} /></span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-sm font-semibold">{n.title}</span>
                      <span className="mt-0.5 block truncate text-sm text-fg-2">{n.body}</span>
                      <span className="mt-1 block text-xs text-fg-3">{timeAgo(n.createdAt)}</span>
                    </span>
                    {!n.read && <><span aria-hidden="true" className="mt-1.5 size-2.5 shrink-0 rounded-full bg-accent" /><span className="sr-only">Unread</span></>}
                  </Link>
                </li>
              );
            })}
          </ul>
        )}
        {q.hasNextPage && (
          <button type="button" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}
            className="mx-auto mt-6 block h-11 rounded-full border border-line bg-surface px-8 text-sm font-semibold hover:bg-line disabled:opacity-60">
            {q.isFetchingNextPage ? "Loading…" : "Show more"}
          </button>
        )}
      </div>
    </div>
  );
}
