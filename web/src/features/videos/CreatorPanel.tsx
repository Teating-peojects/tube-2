"use client";
import { SealCheck } from "@phosphor-icons/react";
import Link from "next/link";
import { useState } from "react";
import { SubscribeButton } from "@/features/creators/SubscribeButton";
import type { VideoDetail } from "@/types";
import { formatViews, timeAgo } from "@/utils/format";

export function CreatorPanel({ video }: { video: VideoDetail }) {
  const [open, setOpen] = useState(false);
  const c = video.creator;
  return (
    <section aria-label="Creator and description" className="space-y-3">
      <div className="flex items-center gap-3">
        <Link href={`/channel/${c.id}`} aria-label={c.name} className="size-11 shrink-0 rounded-full border border-line bg-surface" />
        <div className="min-w-0 flex-1">
          <Link href={`/channel/${c.id}`} className="block truncate font-semibold hover:underline">
            {c.name}
            {c.verified && <SealCheck size={15} weight="fill" className="ml-1 inline align-[-2px] text-fg-2" aria-label="Verified" />}
          </Link>
          <p className="text-xs text-fg-2">{formatViews(c.subscribers)} subscribers</p>
        </div>
        <SubscribeButton<typeof video>
          channelId={c.id} subscribed={c.isSubscribed} queryKey={["video", video.id]}
          apply={(old, next) => ({ ...old, creator: { ...old.creator, isSubscribed: next, subscribers: old.creator.subscribers + (next ? 1 : -1) } })} />
      </div>
      <div className="rounded-card border border-line bg-card p-4 text-sm">
        <p className="mb-1 font-semibold">{formatViews(video.views)} views • {timeAgo(video.publishedAt)}</p>
        {/* Plain text only: React escapes it, no HTML injection. */}
        <p className={`whitespace-pre-line text-fg-2 ${open ? "" : "line-clamp-2"}`}>{video.description}</p>
        <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="mt-2 rounded font-semibold hover:text-accent">
          {open ? "Show less" : "Show more"}
        </button>
      </div>
    </section>
  );
}
