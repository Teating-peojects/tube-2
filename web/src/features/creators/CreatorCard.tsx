import { SealCheck } from "@phosphor-icons/react/dist/ssr";
import Link from "next/link";
import type { ChannelResult } from "@/types";
import { formatViews } from "@/utils/format";

/** Compact vertical creator card (used in horizontal strips). */
export function CreatorCard({ channel }: { channel: ChannelResult }) {
  return (
    <Link href={`/channel/${channel.id}`}
      className="flex w-36 shrink-0 flex-col items-center gap-2 rounded-card border border-line bg-card p-4 text-center transition-colors hover:border-[#3a3a40] hover:bg-surface">
      <span aria-hidden="true" className="size-16 rounded-full border border-line bg-surface" />
      <span className="w-full truncate text-sm font-semibold">
        {channel.name}
        {channel.verified && <SealCheck size={13} weight="fill" className="ml-1 inline align-[-2px] text-fg-2" aria-label="Verified" />}
      </span>
      <span className="text-xs text-fg-2">{formatViews(channel.subscribers)} subscribers</span>
    </Link>
  );
}
