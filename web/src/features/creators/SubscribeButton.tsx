"use client";
import { SpinnerGap } from "@phosphor-icons/react";
import { useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useRequireAuth } from "@/features/auth/useRequireAuth";
import { setSubscription } from "@/services/videos";
import { toast } from "@/store/toast";
import { cn } from "@/utils/format";

type Props<T> = {
  channelId: string;
  subscribed: boolean;
  queryKey: QueryKey; // cache entry holding this channel's state
  apply: (old: T, next: boolean) => T; // pure updater for optimistic UI
};

export function SubscribeButton<T>({ channelId, subscribed, queryKey, apply }: Props<T>) {
  const qc = useQueryClient();
  const requireAuth = useRequireAuth();
  const m = useMutation({
    mutationFn: (next: boolean) => setSubscription(channelId, next),
    onMutate: async (next) => {
      await qc.cancelQueries({ queryKey });
      const prev = qc.getQueryData<T>(queryKey);
      qc.setQueryData<T>(queryKey, (old) => (old ? apply(old, next) : old));
      return { prev };
    },
    onSuccess: (_d, next) => toast.success(next ? "Subscribed" : "Unsubscribed"),
    onError: (_e, _n, ctx) => { if (ctx?.prev) qc.setQueryData(queryKey, ctx.prev); }, // rollback
    onSettled: () => qc.invalidateQueries({ queryKey: ["subscriptions"] }),
  });

  function onClick() {
    if (!requireAuth("Log in to subscribe to creators.")) return;
    m.mutate(!subscribed);
  }

  return (
    <div className="flex flex-col items-start">
      <button type="button" disabled={m.isPending} aria-pressed={subscribed} onClick={onClick}
        className={cn("inline-flex h-10 min-w-28 items-center justify-center gap-2 rounded-full px-5 text-sm font-semibold transition-colors disabled:opacity-70",
          subscribed ? "border border-line bg-surface text-white hover:bg-line" : "bg-accent text-white hover:bg-accent-hover")}>
        {m.isPending && <SpinnerGap size={16} className="animate-spin" aria-hidden="true" />}
        {subscribed ? "Subscribed" : "Subscribe"}
      </button>
      {m.isError && <p role="alert" className="mt-1 text-xs text-accent">Couldn’t update. Try again.</p>}
    </div>
  );
}
