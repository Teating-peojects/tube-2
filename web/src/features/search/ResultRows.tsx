import { ListBullets, SealCheck } from "@phosphor-icons/react/dist/ssr";
import Image from "next/image";
import Link from "next/link";
import type { ChannelResult, PlaylistSummary } from "@/types";
import { formatViews } from "@/utils/format";

export function ChannelRow({ channel: c }: { channel: ChannelResult }) {
  return (
    <article className="flex items-center gap-4 rounded-card border border-line bg-card p-4">
      <Link href={`/channel/${c.id}`} aria-label={c.name} className="size-16 shrink-0 rounded-full border border-line bg-surface sm:size-24" />
      <div className="min-w-0 flex-1">
        <h3 className="truncate text-base font-semibold">
          <Link href={`/channel/${c.id}`} className="hover:underline">{c.name}</Link>
          {c.verified && <SealCheck size={15} weight="fill" className="ml-1 inline align-[-2px] text-fg-2" aria-label="Verified" />}
        </h3>
        <p className="mt-0.5 text-xs text-fg-2">{formatViews(c.subscribers)} subscribers • {c.videoCount} videos</p>
        <p className="mt-1 line-clamp-1 text-xs text-fg-2">{c.description}</p>
      </div>
      <Link href={`/channel/${c.id}`} className="hidden h-10 items-center rounded-full border border-line bg-surface px-5 text-sm font-semibold hover:bg-line sm:inline-flex">View channel</Link>
    </article>
  );
}

export function PlaylistRow({ playlist: p }: { playlist: PlaylistSummary }) {
  return (
    <article className="group flex flex-col gap-3 sm:flex-row sm:gap-4">
      <Link href={`/playlists/${p.id}`} aria-label={`Open playlist ${p.title}`}
        className="relative block aspect-video w-full shrink-0 overflow-hidden rounded-card border border-line bg-gradient-to-br from-[#2b1033] to-card sm:w-64 md:w-80">
        {p.thumbnailUrl && <Image src={p.thumbnailUrl} alt="" fill sizes="(min-width:768px) 320px, 100vw" loading="lazy" className="object-cover" />}
        <span className="absolute inset-y-0 right-0 flex w-1/3 flex-col items-center justify-center gap-1 bg-bg/75 text-xs font-semibold backdrop-blur">
          <ListBullets size={20} aria-hidden="true" />{p.videoCount} videos
        </span>
      </Link>
      <div className="min-w-0 py-0.5">
        <h3 className="line-clamp-2 text-base font-semibold leading-snug"><Link href={`/playlists/${p.id}`}>{p.title}</Link></h3>
        <p className="mt-1 text-xs text-fg-2">{p.creatorName} • Playlist</p>
      </div>
    </article>
  );
}
