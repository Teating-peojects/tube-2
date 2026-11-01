"use client";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { categories } from "@/config/nav";
import { getVideos } from "@/services/videos";
import { cn } from "@/utils/format";
import { videoGridCls as gridCls } from "./grid";
import { VideoCard, VideoCardSkeleton } from "./VideoCard";


type Props = { category?: string; showChips?: boolean };

export function VideoFeed({ category: forced, showChips = true }: Props) {
  const [inner, setInner] = useState("All");
  const category = forced ?? inner;
  const q = useInfiniteQuery({
    queryKey: ["videos", category],
    queryFn: ({ pageParam }) => getVideos({ cursor: pageParam, category }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });

  // Infinite scroll via IntersectionObserver (no scroll listeners).
  const sentinel = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q;
  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const io = new IntersectionObserver(([e]) => { if (e?.isIntersecting && hasNextPage && !isFetchingNextPage) fetchNextPage(); }, { rootMargin: "400px" });
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const videos = q.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <section aria-label="Videos">
      {showChips && <div role="group" aria-label="Categories" className="-mx-3 mb-5 flex gap-2 overflow-x-auto px-3 md:mx-0 md:px-0">
        {categories.map((c) => (
          <button key={c} type="button" onClick={() => setInner(c)} aria-pressed={category === c}
            className={cn("h-9 shrink-0 rounded-full border px-4 text-[13px] font-medium transition-colors",
              category === c ? "border-white bg-white text-bg" : "border-line bg-card text-fg-2 hover:border-[#3a3a40] hover:text-white")}>
            {c}
          </button>
        ))}
      </div>}

      {q.isPending && <div className={gridCls}>{Array.from({ length: 8 }, (_, i) => <VideoCardSkeleton key={i} />)}</div>}

      {q.isError && (
        <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
          <p className="font-semibold">Couldn’t load videos</p>
          <p className="text-sm text-fg-2">Check your connection and try again.</p>
          <button type="button" onClick={() => q.refetch()} className="h-10 rounded-full bg-accent px-5 text-sm font-semibold hover:bg-accent-hover">Retry</button>
        </div>
      )}

      {q.isSuccess && videos.length === 0 && (
        <div className="grid place-items-center gap-2 rounded-card border border-line bg-card py-16 text-center">
          <p className="font-semibold">No videos found</p>
          <p className="text-sm text-fg-2">Try a different category.</p>
        </div>
      )}

      {videos.length > 0 && (
        <div className={gridCls}>
          {videos.map((v, i) => <VideoCard key={v.id} video={v} priority={i < 4} />)}
          {isFetchingNextPage && Array.from({ length: 4 }, (_, i) => <VideoCardSkeleton key={`s${i}`} />)}
        </div>
      )}
      <div ref={sentinel} className="h-px" />
    </section>
  );
}
