"use client";
import { Trash, VideoCamera } from "@phosphor-icons/react";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { Tabs } from "@/components/ui/Tabs";
import { PagedList } from "@/features/library/PagedList";
import { deleteStudioVideo, getStudioVideos, setVideoVisibility } from "@/services/studio";
import type { Paginated, StudioFilter, StudioVideo, StudioVideoStatus, VideoVisibility } from "@/types";
import { cn, formatDuration, formatViews, timeAgo } from "@/utils/format";

type Data = InfiniteData<Paginated<StudioVideo>>;
const KEY = ["studio", "videos"] as const;
const tabs: { id: StudioFilter; label: string }[] = [
  { id: "all", label: "All" }, { id: "published", label: "Published" }, { id: "draft", label: "Drafts" },
  { id: "processing", label: "Processing" }, { id: "private", label: "Private" }, { id: "unlisted", label: "Unlisted" },
];
const statusLabel: Record<StudioVideoStatus, string> = { published: "Published", draft: "Draft", processing: "Processing", failed: "Failed" };

function Row({ v, busy, onVisibility, onDelete }: { v: StudioVideo; busy: boolean; onVisibility: (vis: VideoVisibility) => void; onDelete: () => void }) {
  const [confirm, setConfirm] = useState(false);
  return (
    <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:gap-4">
      <div className="relative aspect-video w-full shrink-0 overflow-hidden rounded-xl border border-line bg-gradient-to-br from-[#3a0a14] to-card sm:w-40">
        {v.durationSec > 0 && <span className="absolute bottom-1.5 right-1.5 rounded bg-bg/80 px-1.5 py-0.5 text-[11px] font-semibold">{formatDuration(v.durationSec)}</span>}
      </div>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold">
          {v.status === "published" ? <Link href={`/watch/${v.id}`} className="hover:underline">{v.title}</Link> : v.title}
        </p>
        <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-2">
          <span className={cn("rounded-full px-2 py-0.5 font-semibold", v.status === "failed" ? "bg-accent/15 text-accent" : "bg-surface")}>{statusLabel[v.status]}</span>
          <span>{timeAgo(v.createdAt)}</span>
        </p>
        <p className="mt-1 text-xs text-fg-3 tabular-nums">{formatViews(v.views)} views • {v.comments} comments • {formatViews(v.likes)} likes</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-2 text-xs text-fg-2">
          <span className="sr-only sm:not-sr-only">Visibility</span>
          <select value={v.visibility} disabled={busy || v.status === "processing"} onChange={(e) => onVisibility(e.target.value as VideoVisibility)}
            aria-label={`Visibility of ${v.title}`} className="h-10 rounded-lg border border-line bg-card px-2 text-sm text-white disabled:opacity-60">
            <option value="public">Public</option><option value="unlisted">Unlisted</option><option value="private">Private</option>
          </select>
        </label>
        {confirm ? (
          <span role="group" aria-label={`Confirm delete ${v.title}`} className="flex items-center gap-2 text-xs">
            <Button variant="secondary" className="h-9 px-3" onClick={() => setConfirm(false)}>Cancel</Button>
            <Button className="h-9 px-3" onClick={onDelete}>Delete</Button>
          </span>
        ) : (
          <button type="button" onClick={() => setConfirm(true)} aria-label={`Delete ${v.title}`} className="grid size-10 place-items-center rounded-full text-fg-2 hover:bg-surface hover:text-white"><Trash size={18} /></button>
        )}
      </div>
    </div>
  );
}

export function VideosView() {
  const qc = useQueryClient();
  const [filter, setFilter] = useState<StudioFilter>("all");

  const optimistic = (fn: (items: StudioVideo[]) => StudioVideo[]) => async () => {
    await qc.cancelQueries({ queryKey: KEY });
    const prev = qc.getQueriesData<Data>({ queryKey: KEY });
    qc.setQueriesData<Data>({ queryKey: KEY }, (old) => old && { ...old, pages: old.pages.map((p) => ({ ...p, items: fn(p.items) })) });
    return { prev };
  };
  const rollback = (_e: unknown, _v: unknown, ctx?: { prev: [readonly unknown[], Data | undefined][] }) => ctx?.prev.forEach(([k, d]) => qc.setQueryData(k, d));
  const settle = () => qc.invalidateQueries({ queryKey: KEY });

  const vis = useMutation({
    mutationFn: (v: { id: string; visibility: VideoVisibility }) => setVideoVisibility(v.id, v.visibility),
    onMutate: (v) => optimistic((items) => items.map((i) => (i.id === v.id ? { ...i, visibility: v.visibility } : i)))(),
    onError: rollback, onSettled: settle,
  });
  const del = useMutation({
    mutationFn: (id: string) => deleteStudioVideo(id),
    onMutate: (id) => optimistic((items) => items.filter((i) => i.id !== id))(),
    onError: rollback, onSettled: settle,
  });

  return (
    <div className="space-y-4">
      <Tabs tabs={tabs} value={filter} onChange={setFilter} label="Filter videos" idPrefix="studio-videos" />
      <div role="tabpanel" id="studio-videos-panel" aria-labelledby={`studio-videos-tab-${filter}`}>
        {(vis.isError || del.isError) && <p role="alert" className="mb-3 text-sm text-accent">Couldn’t update that video. Try again.</p>}
        <PagedList<StudioVideo> layout="rows" queryKey={[...KEY, filter]} fetchPage={(c) => getStudioVideos(filter, c)} getKey={(v) => v.id}
          errorLabel="Couldn’t load your videos"
          empty={<EmptyState icon={<VideoCamera size={28} />} title="No videos here" text="Videos you upload will show up in this list." action={{ label: "Upload a video", href: "/upload" }} />}
          render={(v) => <Row v={v} busy={vis.isPending} onVisibility={(visibility) => vis.mutate({ id: v.id, visibility })} onDelete={() => del.mutate(v.id)} />} />
      </div>
    </div>
  );
}
