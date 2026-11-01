import { Play, SealCheck } from "@phosphor-icons/react/dist/ssr";
import Image from "next/image";
import Link from "next/link";
import type { Video } from "@/types";
import { VideoMenu } from "./VideoMenu";
import { WatchLaterButton } from "./WatchLaterButton";
import { formatDuration, formatViews, timeAgo } from "@/utils/format";

export function VideoCard({ video, priority = false, progress }: { video: Video; priority?: boolean; progress?: number }) {
  const { creator } = video;
  return (
    <article className="group">
      <div className="relative">
        <Link href={`/watch/${video.id}`} aria-label={`Watch ${video.title}`}
          className="relative block aspect-video overflow-hidden rounded-card border border-line bg-gradient-to-br from-[#3a0a14] to-card transition-transform duration-200 group-hover:scale-[1.015]">
          {video.thumbnailUrl && (
            <Image src={video.thumbnailUrl} alt="" fill sizes="(min-width:1280px) 25vw, (min-width:768px) 33vw, 100vw"
              className="object-cover" priority={priority} loading={priority ? undefined : "lazy"} />
          )}
          <span className="absolute inset-0 bg-black/0 transition-colors group-hover:bg-black/45" />
          <span className="absolute left-1/2 top-1/2 grid size-12 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-accent opacity-0 transition-opacity group-hover:opacity-100">
            <Play size={20} weight="fill" aria-hidden="true" />
          </span>
          {video.isLive ? (
            <span className="absolute left-2 top-2 rounded-md bg-accent px-2 py-0.5 text-[11px] font-extrabold">LIVE</span>
          ) : (
            <>
              <span className="absolute bottom-2 left-2 rounded-md bg-bg/80 px-1.5 py-0.5 text-[11px] font-semibold text-accent">{video.quality}</span>
              <span className="absolute bottom-2 right-2 rounded-md bg-bg/80 px-1.5 py-0.5 text-xs font-semibold">{formatDuration(video.durationSec)}</span>
            </>
          )}
          {progress !== undefined && progress > 0 && (
            <span className="absolute inset-x-0 bottom-0 h-1 bg-white/25"><span className="block h-full bg-accent" style={{ width: `${Math.min(100, progress * 100)}%` }} /></span>
          )}
        </Link>
        <WatchLaterButton videoId={video.id} />
      </div>
      <div className="mt-3 flex gap-3">
        <Link href={`/channel/${creator.id}`} aria-label={creator.name} className="size-9 shrink-0 rounded-full border border-line bg-surface" />
        <div className="min-w-0 flex-1">
          <h3 className="line-clamp-2 text-sm font-semibold leading-snug">
            <Link href={`/watch/${video.id}`}>{video.title}</Link>
          </h3>
          <p className="mt-1 text-xs leading-relaxed text-fg-2">
            <Link href={`/channel/${creator.id}`} className="hover:text-white">{creator.name}</Link>
            {creator.verified && <SealCheck size={13} weight="fill" className="ml-1 inline align-[-2px]" aria-label="Verified" />}
            <br />
            {video.isLive ? "Streaming now" : `${formatViews(video.views)} views • ${timeAgo(video.publishedAt)}`}
          </p>
        </div>
        <VideoMenu video={video} />
      </div>
    </article>
  );
}

export function VideoCardSkeleton() {
  return (
    <div aria-hidden="true">
      <div className="skeleton aspect-video rounded-card" />
      <div className="mt-3 flex gap-3">
        <div className="skeleton size-9 rounded-full" />
        <div className="flex-1 space-y-2"><div className="skeleton h-3.5 w-11/12 rounded" /><div className="skeleton h-3 w-1/2 rounded" /></div>
      </div>
    </div>
  );
}
