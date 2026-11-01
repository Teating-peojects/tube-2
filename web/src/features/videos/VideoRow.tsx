import { Play, SealCheck } from "@phosphor-icons/react/dist/ssr";
import Image from "next/image";
import Link from "next/link";
import type { Video } from "@/types";
import { formatDuration, formatViews, timeAgo } from "@/utils/format";

/** List-style video result. Desktop: thumbnail left, info right. Mobile: stacked. */
export function VideoRow({ video, rank, progress }: { video: Video; rank?: number; progress?: number }) {
  const { creator } = video;
  return (
    <article className="group flex flex-col gap-3 sm:flex-row sm:gap-4">
      {rank !== undefined && (
        <span aria-hidden="true" className="hidden w-8 shrink-0 self-center text-right text-xl font-extrabold tabular-nums text-fg-3 sm:block">{rank}</span>
      )}
      <Link href={`/watch/${video.id}`} aria-label={`Watch ${video.title}`}
        className="relative block aspect-video w-full shrink-0 overflow-hidden rounded-card border border-line bg-gradient-to-br from-[#3a0a14] to-card sm:w-64 md:w-80">
        {video.thumbnailUrl && <Image src={video.thumbnailUrl} alt="" fill sizes="(min-width:768px) 320px, (min-width:640px) 256px, 100vw" loading="lazy" className="object-cover" />}
        <span className="absolute inset-0 bg-black/0 transition-colors group-hover:bg-black/40" />
        <span className="absolute left-1/2 top-1/2 grid size-11 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-accent opacity-0 transition-opacity group-hover:opacity-100">
          <Play size={18} weight="fill" aria-hidden="true" />
        </span>
        {video.isLive ? (
          <span className="absolute left-2 top-2 rounded-md bg-accent px-2 py-0.5 text-[11px] font-extrabold">LIVE</span>
        ) : (
          <>
            <span className="absolute bottom-2 left-2 rounded-md bg-bg/80 px-1.5 py-0.5 text-[11px] font-semibold text-accent">{video.quality}</span>
            <span className="absolute bottom-2 right-2 rounded-md bg-bg/80 px-1.5 py-0.5 text-xs font-semibold">{formatDuration(video.durationSec)}</span>
          </>
        )}
        {rank !== undefined && <span aria-hidden="true" className="absolute right-2 top-2 rounded-md bg-bg/80 px-2 py-0.5 text-xs font-bold sm:hidden">#{rank}</span>}
        {progress !== undefined && progress > 0 && (
          <span className="absolute inset-x-0 bottom-0 h-1 bg-white/25"><span className="block h-full bg-accent" style={{ width: `${Math.min(100, progress * 100)}%` }} /></span>
        )}
      </Link>
      <div className="min-w-0 py-0.5">
        <h3 className="line-clamp-2 text-base font-semibold leading-snug"><Link href={`/watch/${video.id}`}>{video.title}</Link></h3>
        <p className="mt-1 text-xs text-fg-2">{video.isLive ? "Streaming now" : `${formatViews(video.views)} views • ${timeAgo(video.publishedAt)}`}</p>
        <Link href={`/channel/${creator.id}`} className="mt-2 inline-flex items-center gap-2 text-xs text-fg-2 hover:text-white">
          <span aria-hidden="true" className="size-6 rounded-full border border-line bg-surface" />
          {creator.name}
          {creator.verified && <SealCheck size={13} weight="fill" aria-label="Verified" />}
        </Link>
      </div>
    </article>
  );
}

export function VideoRowSkeleton() {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:gap-4" aria-hidden="true">
      <div className="skeleton aspect-video w-full shrink-0 rounded-card sm:w-64 md:w-80" />
      <div className="flex-1 space-y-2.5 py-1"><div className="skeleton h-4 w-4/5 rounded" /><div className="skeleton h-3 w-1/3 rounded" /><div className="skeleton h-3 w-1/4 rounded" /></div>
    </div>
  );
}
