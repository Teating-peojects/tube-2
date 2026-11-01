"use client";
import { ShareNetwork, ThumbsDown, ThumbsUp } from "@phosphor-icons/react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AddToPlaylist } from "@/features/playlists/AddToPlaylist";
import { setReaction } from "@/services/videos";
import { modals } from "@/store/modal";
import type { Reaction, VideoDetail } from "@/types";
import { cn, formatViews } from "@/utils/format";

export function applyReaction(v: VideoDetail, next: Reaction): VideoDetail {
  return {
    ...v,
    reaction: next,
    likes: v.likes - (v.reaction === "like" ? 1 : 0) + (next === "like" ? 1 : 0),
    dislikes: v.dislikes - (v.reaction === "dislike" ? 1 : 0) + (next === "dislike" ? 1 : 0),
  };
}

const pill = "inline-flex h-10 items-center gap-2 px-4 text-sm font-semibold transition-colors hover:bg-line disabled:opacity-60";

export function ActionBar({ video }: { video: VideoDetail }) {
  const qc = useQueryClient();
  const key = ["video", video.id];

  const react = useMutation({
    mutationFn: (r: Reaction) => setReaction(video.id, r),
    onMutate: async (r) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<VideoDetail>(key);
      qc.setQueryData<VideoDetail>(key, (old) => (old ? applyReaction(old, r) : old));
      return { prev };
    },
    onError: (_e, _r, ctx) => { if (ctx?.prev) qc.setQueryData(key, ctx.prev); }, // rollback
  });

  const toggle = (kind: "like" | "dislike") => react.mutate(video.reaction === kind ? null : kind);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="flex overflow-hidden rounded-full border border-line bg-surface" role="group" aria-label="Rate this video">
        <button type="button" disabled={react.isPending} aria-pressed={video.reaction === "like"} onClick={() => toggle("like")}
          className={cn(pill, "border-r border-line", video.reaction === "like" && "text-accent")}>
          <ThumbsUp size={20} weight={video.reaction === "like" ? "fill" : "regular"} aria-hidden="true" />
          <span aria-label={`${video.likes} likes`}>{formatViews(video.likes)}</span>
        </button>
        <button type="button" disabled={react.isPending} aria-pressed={video.reaction === "dislike"} onClick={() => toggle("dislike")}
          aria-label="Dislike" className={cn(pill, video.reaction === "dislike" && "text-accent")}>
          <ThumbsDown size={20} weight={video.reaction === "dislike" ? "fill" : "regular"} aria-hidden="true" />
        </button>
      </div>
      <button type="button" onClick={() => modals.share({ url: `${location.origin}/watch/${video.id}`, title: video.title })} className={cn(pill, "rounded-full border border-line bg-surface")}>
        <ShareNetwork size={20} aria-hidden="true" />Share
      </button>
      <AddToPlaylist videoId={video.id} />
      {react.isError && <p role="alert" className="text-xs text-accent">Couldn’t save your reaction.</p>}
    </div>
  );
}
