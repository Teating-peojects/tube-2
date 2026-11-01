"use client";
import { useQuery } from "@tanstack/react-query";
import { useSession } from "@/features/auth/useSession";
import { getUnreadCount } from "@/services/notifications";

export const unreadKey = ["unread-count"] as const;

/** Polls gently (60s, only while the tab is visible). A realtime channel can replace this later. */
export function useUnreadCount() {
  const { status } = useSession();
  return useQuery({ queryKey: unreadKey, queryFn: getUnreadCount, enabled: status === "authenticated", staleTime: 30_000, refetchInterval: 60_000 });
}
