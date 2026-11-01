"use client";
import { useQueryClient } from "@tanstack/react-query";
import { useSession } from "@/features/auth/useSession";
import { useRealtime } from "@/lib/realtime";
import { unreadKey } from "./useUnread";

/** Keeps notifications fresh when a WebSocket is configured (no-op otherwise; polling still runs). */
export function RealtimeBridge() {
  const qc = useQueryClient();
  const { status } = useSession();
  useRealtime(status === "authenticated" ? "notifications" : null, () => {
    void qc.invalidateQueries({ queryKey: ["notifications"] });
    void qc.invalidateQueries({ queryKey: unreadKey });
  });
  return null;
}
