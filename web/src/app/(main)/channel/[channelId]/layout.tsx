import type { Metadata } from "next";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";
import { ChannelHeader } from "@/features/channels/ChannelHeader";
import { ChannelNav } from "@/features/channels/ChannelNav";
import { loadChannel } from "@/features/channels/load";

type Props = { children: ReactNode; params: Promise<{ channelId: string }> };

export async function generateMetadata({ params }: Pick<Props, "params">): Promise<Metadata> {
  const { channelId } = await params;
  const c = await loadChannel(channelId);
  if (!c) return { title: "Channel not found", robots: { index: false } };
  const description = c.description.slice(0, 160);
  return {
    title: c.name, description,
    alternates: { canonical: `/channel/${c.id}` },
    openGraph: { type: "profile", title: c.name, description, url: `/channel/${c.id}`, images: c.bannerUrl ? [c.bannerUrl] : undefined },
  };
}

export default async function ChannelLayout({ children, params }: Props) {
  const { channelId } = await params;
  const channel = await loadChannel(channelId);
  if (!channel) notFound();
  return (
    <div className="mx-auto max-w-[1500px]">
      <ChannelHeader initial={channel} />
      <ChannelNav channelId={channel.id} />
      <div className="pt-6">{children}</div>
    </div>
  );
}
