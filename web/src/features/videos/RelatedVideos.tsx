"use client";
import { SealCheck } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import Image from "next/image";
import Link from "next/link";
import { getVideos } from "@/services/videos";
import { useUIStore } from "@/store/ui";
import type { Video } from "@/types";
import { formatDuration, formatViews, timeAgo } from "@/utils/format";

function Row({ v }: { v: Video }) {
  return (
    <li>
      <Link href={`/watch/${v.id}`} className="group flex gap-3">
        <span className="relative aspect-video w-40 shrink-0 overflow-hidden rounded-xl border border-line bg-gradient-to-br from-[#3a0a14] to-card sm:w-44">
          {v.thumbnailUrl && <Image src={v.thumbnailUrl} alt="" fill sizes="176px" loading="lazy" className="object-cover" />}
          <span className="absolute bottom-1.5 right-1.5 rounded bg-bg/80 px-1.5 py-0.5 text-[11px] font-semibold">
            {v.isLive ? "LIVE" : formatDuration(v.durationSec)}
          </span>
        </span>
        <span className="min-w-0">
          <span className="line-clamp-2 text-sm font-semibold leading-snug group-hover:text-white">{v.title}</span>
          <span className="mt-1 block text-xs text-fg-2">
            {v.creator.name}
            {v.creator.verified && <SealCheck size={12} weight="fill" className="ml-1 inline align-[-2px]" aria-label="Verified" />}
            <br />{formatViews(v.views)} views • {timeAgo(v.publishedAt)}
          </span>
        </span>
      </Link>
    </li>
  );
}

function RowSkeleton() {
  return (
    <li className="flex gap-3" aria-hidden="true">
      <div className="skeleton aspect-video w-40 shrink-0 rounded-xl sm:w-44" />
      <div className="flex-1 space-y-2"><div className="skeleton h-3.5 w-11/12 rounded" /><div className="skeleton h-3 w-1/2 rounded" /></div>
    </li>
  );
}

export function RelatedVideos({ currentId }: { currentId: string }) {
  const q = useQuery({ queryKey: ["related", currentId], queryFn: () => getVideos({}) });
  const autoplay = useUIStore((s) => s.autoplayNext);
  const setAutoplay = useUIStore((s) => s.setAutoplayNext);
  const list = (q.data?.items ?? []).filter((v) => v.id !== currentId);
  const [next, ...rest] = list;

  return (
    <aside aria-label="Related videos" className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-bold">Up next</h2>
        <label className="flex cursor-pointer items-center gap-2 text-xs text-fg-2">
          Autoplay
          <input type="checkbox" role="switch" checked={autoplay} onChange={(e) => setAutoplay(e.target.checked)} className="size-4 accent-accent" />
        </label>
      </div>
      {q.isPending && <ul className="space-y-3">{Array.from({ length: 6 }, (_, i) => <RowSkeleton key={i} />)}</ul>}
      {q.isError && (
        <div role="alert" className="rounded-card border border-line bg-card p-4 text-sm">
          Couldn’t load related videos.{" "}
          <button type="button" onClick={() => q.refetch()} className="font-semibold text-accent hover:underline">Retry</button>
        </div>
      )}
      {next && (
        <>
          <ul className="rounded-card border border-line bg-card p-2"><Row v={next} /></ul>
          <h2 className="pt-1 text-base font-bold">Related</h2>
          <ul className="space-y-3">{rest.slice(0, 11).map((v) => <Row key={v.id} v={v} />)}</ul>
        </>
      )}
    </aside>
  );
}
