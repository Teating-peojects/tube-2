"use client";
import { FilmStrip } from "@phosphor-icons/react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { VideoPlayer } from "@/components/video/VideoPlayer";
import { EmptyState } from "@/components/ui/EmptyState";
import { VideoMenu } from "@/features/videos/VideoMenu";
import { getShorts, getVideo } from "@/services/videos";
import type { Video } from "@/types";
import { formatViews } from "@/utils/format";

function ShortCard({ video }: { video: Video }) {
  const ref = useRef<HTMLElement>(null);
  const [near, setNear] = useState(false);

  // Only fetch the stream details when the card is about to be seen.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(([e]) => { if (e?.isIntersecting) { setNear(true); io.disconnect(); } }, { rootMargin: "300px" });
    io.observe(el);
    return () => io.disconnect();
  }, []);
  const detail = useQuery({ queryKey: ["video", video.id], queryFn: () => getVideo(video.id), enabled: near });

  return (
    <article ref={ref} className="mx-auto w-[min(100%,calc((100dvh-10rem)*9/16))]">
      {detail.isPending ? <div className="skeleton aspect-[9/16] rounded-card" aria-hidden="true" />
        : <VideoPlayer src={detail.data?.streamUrl ?? null} poster={detail.data?.posterUrl} title={video.title} aspectClass="aspect-[9/16]" />}
      <div className="mt-3 flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="line-clamp-2 text-sm font-semibold"><Link href={`/watch/${video.id}`}>{video.title}</Link></h2>
          <p className="mt-1 text-xs text-fg-2"><Link href={`/channel/${video.creator.id}`} className="hover:text-white">{video.creator.name}</Link> • {formatViews(video.views)} views</p>
        </div>
        <VideoMenu video={video} />
      </div>
    </article>
  );
}

export function ShortsFeed() {
  const q = useInfiniteQuery({
    queryKey: ["shorts"], queryFn: ({ pageParam }) => getShorts(pageParam), initialPageParam: undefined as string | undefined,
    getNextPageParam: (l) => l.nextCursor ?? undefined,
  });
  const sentinel = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q;
  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const io = new IntersectionObserver(([e]) => { if (e?.isIntersecting && hasNextPage && !isFetchingNextPage) void fetchNextPage(); }, { rootMargin: "600px" });
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="space-y-8 pb-8">
      <h1 className="sr-only">Shorts</h1>
      {q.isPending && <div className="skeleton mx-auto aspect-[9/16] w-[min(100%,calc((100dvh-10rem)*9/16))] rounded-card" aria-hidden="true" />}
      {q.isError && (
        <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
          <p className="font-semibold">Couldn’t load shorts</p>
          <button type="button" onClick={() => q.refetch()} className="h-10 rounded-full bg-accent px-5 text-sm font-semibold hover:bg-accent-hover">Retry</button>
        </div>
      )}
      {q.isSuccess && items.length === 0 && <EmptyState icon={<FilmStrip size={28} />} title="No shorts yet" text="Short videos will appear here." />}
      {items.map((v) => <ShortCard key={v.id} video={v} />)}
      <div ref={sentinel} className="h-px" />
    </div>
  );
}
