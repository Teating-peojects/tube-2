"use client";
import { SealCheck } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import Image from "next/image";
import { SubscribeButton } from "@/features/creators/SubscribeButton";
import { safeExternalUrl } from "@/lib/auth";
import { getChannel } from "@/services/channels";
import type { ChannelDetail } from "@/types";
import { formatViews } from "@/utils/format";

export function ChannelHeader({ initial }: { initial: ChannelDetail }) {
  const { data: c } = useQuery({ queryKey: ["channel", initial.id], queryFn: () => getChannel(initial.id), initialData: initial });
  const links = c.links.map((l) => ({ label: l.label, url: safeExternalUrl(l.url) })).filter((l): l is { label: string; url: string } => Boolean(l.url));

  return (
    <header className="-mx-3 -mt-4 md:-mx-7 lg:-mt-6">
      <div className="relative h-32 overflow-hidden bg-gradient-to-br from-[#4a0b1c] via-card to-bg sm:h-48 lg:h-56">
        {c.bannerUrl && <Image src={c.bannerUrl} alt="" fill priority sizes="100vw" className="object-cover" />}
      </div>
      <div className="px-3 md:px-7">
        <div className="-mt-10 flex flex-wrap items-end gap-x-5 gap-y-3 sm:-mt-12">
          <span aria-hidden="true" className="size-20 shrink-0 rounded-full border-4 border-bg bg-surface sm:size-28" />
          <div className="min-w-0 flex-1 pb-1">
            <h1 className="flex items-center gap-2 truncate text-2xl font-extrabold">
              {c.name}
              {c.verified && <SealCheck size={20} weight="fill" className="shrink-0 text-fg-2" aria-label="Verified" />}
            </h1>
            <p className="mt-0.5 text-sm text-fg-2">{formatViews(c.subscribers)} subscribers • {c.videoCount} videos</p>
            <p className="mt-1 line-clamp-2 max-w-2xl text-sm text-fg-2">{c.description}</p>
          </div>
          <div className="pb-1">
            <SubscribeButton<ChannelDetail> channelId={c.id} subscribed={c.isSubscribed} queryKey={["channel", c.id]}
              apply={(old, next) => ({ ...old, isSubscribed: next, subscribers: old.subscribers + (next ? 1 : -1) })} />
          </div>
        </div>
        {links.length > 0 && (
          <ul className="mt-3 flex flex-wrap gap-2 text-xs" aria-label="Links">
            {links.map((l) => (
              <li key={l.url}><a href={l.url} target="_blank" rel="noopener noreferrer nofollow ugc" className="inline-flex h-8 items-center rounded-full border border-line bg-card px-3 text-fg-2 hover:text-white">{l.label}</a></li>
            ))}
          </ul>
        )}
      </div>
    </header>
  );
}
