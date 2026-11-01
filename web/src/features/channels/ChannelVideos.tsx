"use client";
import { FilmStrip, Broadcast, VideoCamera } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { EmptyState } from "@/components/ui/EmptyState";
import { PagedList } from "@/features/library/PagedList";
import { VideoCard, VideoCardSkeleton } from "@/features/videos/VideoCard";
import { videoGridCls } from "@/features/videos/grid";
import { getChannelPlaylists, getChannelVideos } from "@/services/channels";
import type { ChannelVideoKind, Video } from "@/types";
import { PlaylistCard } from "@/features/playlists/PlaylistCard";

const empties = {
  videos: { icon: <VideoCamera size={28} />, title: "No videos yet", text: "This channel hasn’t uploaded any videos." },
  shorts: { icon: <FilmStrip size={28} />, title: "No shorts yet", text: "Short-form videos from this channel will appear here." },
  live: { icon: <Broadcast size={28} />, title: "Not live right now", text: "Live streams from this channel will appear here." },
} as const;

export function ChannelVideos({ channelId, kind }: { channelId: string; kind: ChannelVideoKind }) {
  const e = empties[kind];
  return (
    <PagedList<Video> layout="grid" queryKey={["channel", channelId, "videos", kind]} getKey={(v) => v.id}
      fetchPage={(cursor) => getChannelVideos(channelId, { kind, sort: "latest", cursor })}
      errorLabel="Couldn’t load videos" empty={<EmptyState icon={e.icon} title={e.title} text={e.text} />}
      render={(v, i) => <VideoCard video={v} priority={i < 4} />} />
  );
}

function Preview({ channelId, title, sort, count }: { channelId: string; title: string; sort: "latest" | "popular"; count: number }) {
  const q = useQuery({ queryKey: ["channel", channelId, "preview", sort], queryFn: () => getChannelVideos(channelId, { kind: "videos", sort }) });
  if (q.isSuccess && q.data.items.length === 0) return null;
  return (
    <section aria-label={title}>
      <h2 className="mb-4 text-lg font-bold">{title}</h2>
      <div className={videoGridCls}>
        {q.isPending && Array.from({ length: count }, (_, i) => <VideoCardSkeleton key={i} />)}
        {q.isError && <p role="alert" className="text-sm text-fg-2">Couldn’t load videos. <button type="button" onClick={() => q.refetch()} className="font-semibold text-accent hover:underline">Retry</button></p>}
        {q.data?.items.slice(0, count).map((v) => <VideoCard key={v.id} video={v} />)}
      </div>
    </section>
  );
}

export function ChannelHome({ channelId }: { channelId: string }) {
  const probe = useQuery({ queryKey: ["channel", channelId, "preview", "latest"], queryFn: () => getChannelVideos(channelId, { kind: "videos", sort: "latest" }) });
  if (probe.isSuccess && probe.data.items.length === 0) return <EmptyState icon={<VideoCamera size={28} />} title="No videos yet" text="This channel hasn’t uploaded any videos." />;
  return (
    <div className="space-y-10">
      <Preview channelId={channelId} title="Latest uploads" sort="latest" count={8} />
      <Preview channelId={channelId} title="Popular" sort="popular" count={4} />
    </div>
  );
}

export function ChannelPlaylists({ channelId }: { channelId: string }) {
  const q = useQuery({ queryKey: ["channel", channelId, "playlists"], queryFn: () => getChannelPlaylists(channelId) });
  if (q.isPending) return <div className={videoGridCls} aria-hidden="true">{Array.from({ length: 4 }, (_, i) => <div key={i} className="skeleton aspect-video rounded-card" />)}</div>;
  if (q.isError) return <div role="alert" className="rounded-card border border-line bg-card py-12 text-center text-sm">Couldn’t load playlists. <button type="button" onClick={() => q.refetch()} className="font-semibold text-accent hover:underline">Retry</button></div>;
  if (q.data.length === 0) return <EmptyState icon={<VideoCamera size={28} />} title="No public playlists" text="This channel hasn’t shared any playlists." />;
  return <ul className={videoGridCls}>{q.data.map((p) => <li key={p.id}><PlaylistCard playlist={p} /></li>)}</ul>;
}
