"use client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRequireAuth } from "@/features/auth/useRequireAuth";
import { useSession } from "@/features/auth/useSession";
import { getWatchLaterIds, setWatchLater } from "@/services/library";
import { toast } from "@/store/toast";

export const wlIdsKey = ["watch-later", "ids"] as const;
export const wlListKey = ["watch-later", "list"] as const;

export function useToggleWatchLater(videoId: string) {
  const qc = useQueryClient();
  const requireAuth = useRequireAuth();
  const { status } = useSession();
  const ids = useQuery({ queryKey: wlIdsKey, queryFn: getWatchLaterIds, enabled: status === "authenticated", staleTime: 5 * 60_000 });
  const saved = ids.data?.includes(videoId) ?? false;

  const m = useMutation({
    mutationFn: (next: boolean) => setWatchLater(videoId, next),
    onMutate: async (next) => {
      await qc.cancelQueries({ queryKey: wlIdsKey });
      const prev = qc.getQueryData<string[]>(wlIdsKey) ?? [];
      qc.setQueryData<string[]>(wlIdsKey, next ? [...new Set([...prev, videoId])] : prev.filter((i) => i !== videoId));
      return { prev };
    },
    onSuccess: (_d, next) => toast.success(next ? "Added to Watch Later" : "Removed from Watch Later"),
    onError: (_e, _n, ctx) => { if (ctx) qc.setQueryData(wlIdsKey, ctx.prev); toast.error("Couldn’t update Watch Later"); }, // rollback
    onSettled: () => qc.invalidateQueries({ queryKey: wlListKey }),
  });

  function toggle() {
    if (!requireAuth("Log in to save videos to Watch Later.")) return;
    m.mutate(!saved);
  }
  return { saved, loaded: ids.isSuccess, toggle, pending: m.isPending, failed: m.isError };
}
