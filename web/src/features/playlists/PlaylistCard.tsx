import { ListBullets, Lock } from "@phosphor-icons/react/dist/ssr";
import Image from "next/image";
import Link from "next/link";
import type { Playlist } from "@/types";
import { timeAgo } from "@/utils/format";

export function PlaylistCard({ playlist: p }: { playlist: Playlist }) {
  return (
    <article className="group">
      <Link href={`/playlists/${p.id}`} aria-label={`Open playlist ${p.title}`}
        className="relative block aspect-video overflow-hidden rounded-card border border-line bg-gradient-to-br from-[#2b1033] to-card transition-transform duration-200 group-hover:scale-[1.015]">
        {p.thumbnailUrl && <Image src={p.thumbnailUrl} alt="" fill sizes="(min-width:1280px) 25vw, (min-width:768px) 33vw, 100vw" loading="lazy" className="object-cover" />}
        <span className="absolute inset-y-0 right-0 flex w-2/5 flex-col items-center justify-center gap-1 bg-bg/75 text-xs font-semibold backdrop-blur">
          <ListBullets size={22} aria-hidden="true" />{p.videoCount} {p.videoCount === 1 ? "video" : "videos"}
        </span>
      </Link>
      <h3 className="mt-3 line-clamp-2 text-sm font-semibold leading-snug">
        <Link href={`/playlists/${p.id}`}>{p.title}</Link>
        {p.visibility === "private" && <Lock size={13} className="ml-1.5 inline align-[-2px] text-fg-2" aria-label="Private" />}
      </h3>
      <p className="mt-1 text-xs text-fg-2">{p.ownerName} • Updated {timeAgo(p.updatedAt)}</p>
    </article>
  );
}
