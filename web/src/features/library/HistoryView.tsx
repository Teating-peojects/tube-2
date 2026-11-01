"use client";
import { ClockCounterClockwise, X } from "@phosphor-icons/react";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { VideoRow } from "@/features/videos/VideoRow";
import { clearHistory, getHistory, removeHistoryItem } from "@/services/library";
import { toast } from "@/store/toast";
import type { HistoryItem, Paginated } from "@/types";
import { timeAgo } from "@/utils/format";
import { PagedList } from "./PagedList";

type Data = InfiniteData<Paginated<HistoryItem>>;

export function HistoryView() {
  const qc = useQueryClient();
  const [confirm, setConfirm] = useState(false);

  const remove = useMutation({
    mutationFn: removeHistoryItem,
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: ["history"] });
      const prev = qc.getQueriesData<Data>({ queryKey: ["history"] });
      qc.setQueriesData<Data>({ queryKey: ["history"] }, (old) => old && { ...old, pages: old.pages.map((p) => ({ ...p, items: p.items.filter((i) => i.video.id !== id) })) });
      return { prev };
    },
    onError: (_e, _id, ctx) => ctx?.prev.forEach(([k, d]) => qc.setQueryData(k, d)), // rollback
    onSettled: () => qc.invalidateQueries({ queryKey: ["continue-watching"] }),
  });
  const clear = useMutation({
    mutationFn: clearHistory,
    onSuccess: () => { toast.success("Watch history cleared"); setConfirm(false); void qc.invalidateQueries({ queryKey: ["history"] }); void qc.invalidateQueries({ queryKey: ["continue-watching"] }); },
  });

  return (
    <div className="mx-auto max-w-5xl">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-extrabold">Watch history</h1>
        {confirm ? (
          <div role="group" aria-label="Confirm clearing history" className="flex items-center gap-2 text-sm">
            <span className="text-fg-2">Clear all history?</span>
            <Button variant="secondary" className="h-9 px-4" onClick={() => setConfirm(false)}>Cancel</Button>
            <Button className="h-9 px-4" loading={clear.isPending} onClick={() => clear.mutate()}>Clear</Button>
          </div>
        ) : (
          <Button variant="secondary" className="h-10" onClick={() => setConfirm(true)}>Clear all history</Button>
        )}
      </div>
      {(remove.isError || clear.isError) && <p role="alert" className="mb-4 text-sm text-accent">Couldn’t update your history. Try again.</p>}
      <PagedList<HistoryItem>
        queryKey={["history"]} fetchPage={getHistory} getKey={(h) => h.video.id}
        errorLabel="Couldn’t load your history"
        empty={<EmptyState icon={<ClockCounterClockwise size={28} />} title="No watch history" text="Videos you watch will show up here." action={{ label: "Browse videos", href: "/" }} />}
        render={(h) => (
          <div className="relative">
            <VideoRow video={h.video} progress={h.progressSec / Math.max(1, h.video.durationSec)} />
            <p className="mt-1 text-xs text-fg-3 sm:ml-0 sm:pl-0">Watched {timeAgo(h.watchedAt)}</p>
            <button type="button" onClick={() => remove.mutate(h.video.id)} aria-label={`Remove ${h.video.title} from history`}
              className="absolute right-0 top-0 grid size-9 place-items-center rounded-full text-fg-2 hover:bg-surface hover:text-white sm:top-1"><X size={16} /></button>
          </div>
        )} />
    </div>
  );
}
