"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Tabs } from "@/components/ui/Tabs";
import { CreatorCard } from "@/features/creators/CreatorCard";
import { VideoRow, VideoRowSkeleton } from "@/features/videos/VideoRow";
import { getPopularCreators, getTrending } from "@/services/trending";
import type { TrendingTab } from "@/types";

const tabs: { id: TrendingTab; label: string }[] = [
  { id: "now", label: "Trending Now" },
  { id: "most-viewed", label: "Most Viewed" },
  { id: "fast-growing", label: "Fast Growing" },
  { id: "recent", label: "Recently Trending" },
];

export function TrendingView() {
  const [tab, setTab] = useState<TrendingTab>("now");

  const creators = useQuery({ queryKey: ["popular-creators"], queryFn: getPopularCreators, staleTime: 10 * 60_000 });
  const q = useInfiniteQuery({
    queryKey: ["trending", tab],
    queryFn: ({ pageParam }) => getTrending(tab, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
  const videos = q.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="mx-auto max-w-5xl space-y-8">
      <h1 className="text-2xl font-extrabold">Trending</h1>

      <section aria-labelledby="creators-h">
        <h2 id="creators-h" className="mb-3 text-base font-bold">Popular Creators</h2>
        <div className="-mx-3 flex gap-3 overflow-x-auto px-3 pb-1 md:mx-0 md:px-0">
          {creators.isPending && Array.from({ length: 6 }, (_, i) => <div key={i} className="skeleton h-36 w-36 shrink-0 rounded-card" aria-hidden="true" />)}
          {creators.isError && (
            <p role="alert" className="text-sm text-fg-2">Couldn’t load creators. <button type="button" onClick={() => creators.refetch()} className="font-semibold text-accent hover:underline">Retry</button></p>
          )}
          {creators.data?.map((c) => <CreatorCard key={c.id} channel={c} />)}
        </div>
      </section>

      <section aria-label="Trending videos">
        <Tabs tabs={tabs} value={tab} onChange={setTab} label="Trending categories" idPrefix="trending" />
        <div role="tabpanel" id="trending-panel" aria-labelledby={`trending-tab-${tab}`} tabIndex={0} className="pt-6">
          {q.isPending && <div className="space-y-5">{Array.from({ length: 5 }, (_, i) => <VideoRowSkeleton key={i} />)}</div>}

          {q.isError && (
            <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
              <p className="font-semibold">Couldn’t load trending videos</p>
              <button type="button" onClick={() => q.refetch()} className="h-10 rounded-full bg-accent px-5 text-sm font-semibold hover:bg-accent-hover">Retry</button>
            </div>
          )}

          {q.isSuccess && videos.length === 0 && (
            <p className="rounded-card border border-line bg-card py-16 text-center text-sm text-fg-2">Nothing is trending right now. Check back soon.</p>
          )}

          {videos.length > 0 && (
            <ol className="space-y-5">
              {videos.map((v, i) => <li key={v.id}><VideoRow video={v} rank={i + 1} /></li>)}
            </ol>
          )}

          {q.hasNextPage && (
            <button type="button" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}
              className="mx-auto mt-8 block h-11 rounded-full border border-line bg-surface px-8 text-sm font-semibold hover:bg-line disabled:opacity-60">
              {q.isFetchingNextPage ? "Loading…" : "Show more"}
            </button>
          )}
        </div>
      </section>
    </div>
  );
}
