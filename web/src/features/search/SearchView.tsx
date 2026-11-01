"use client";
import { MagnifyingGlass } from "@phosphor-icons/react";
import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { VideoRow, VideoRowSkeleton } from "@/features/videos/VideoRow";
import { DEFAULTS, parseSearchParams, toQueryString } from "@/lib/search-params";
import { searchContent } from "@/services/search";
import type { SearchParams } from "@/types";
import { SearchBox } from "./SearchBox";
import { SearchFilters } from "./SearchFilters";
import { ChannelRow, PlaylistRow } from "./ResultRows";

export function SearchPageSkeleton() {
  return <div className="mx-auto max-w-5xl space-y-5" aria-hidden="true">{Array.from({ length: 5 }, (_, i) => <VideoRowSkeleton key={i} />)}</div>;
}

export function SearchView() {
  const router = useRouter();
  const params = parseSearchParams(useSearchParams());
  const hasQuery = params.q.length > 0;

  const q = useInfiniteQuery({
    queryKey: ["search", params],
    queryFn: ({ pageParam }) => searchContent(params, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: hasQuery,
    placeholderData: keepPreviousData,
  });

  function change<K extends keyof Omit<SearchParams, "q">>(key: K, value: SearchParams[K]) {
    router.replace(`/search?${toQueryString({ ...params, [key]: value })}`, { scroll: false });
  }
  const filtersActive = (["sort", "type", "duration", "date"] as const).some((k) => params[k] !== DEFAULTS[k]);

  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  const total = q.data?.pages[0]?.total ?? 0;

  return (
    <div className="mx-auto max-w-5xl">
      <h1 className="sr-only">Search</h1>
      <div className="mb-5 md:hidden"><SearchBox autoFocus={!hasQuery} /></div>

      {!hasQuery ? (
        <div className="grid place-items-center gap-3 py-20 text-center">
          <span className="grid size-16 place-items-center rounded-full border border-line bg-card"><MagnifyingGlass size={28} className="text-fg-2" aria-hidden="true" /></span>
          <p className="text-lg font-bold">Search REDXTUBE</p>
          <p className="max-w-sm text-sm text-fg-2">Find videos, creators, playlists and live streams.</p>
        </div>
      ) : (
        <>
          <SearchFilters params={params} onChange={change} />
          <p className="mb-4 mt-5 text-sm text-fg-2" role="status" aria-live="polite">
            {q.isSuccess ? `${total.toLocaleString("en")} ${total === 1 ? "result" : "results"} for “${params.q}”` : ""}
          </p>

          {q.isPending && <SearchPageSkeleton />}

          {q.isError && (
            <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
              <p className="font-semibold">Search failed</p>
              <p className="text-sm text-fg-2">Check your connection and try again.</p>
              <button type="button" onClick={() => q.refetch()} className="h-10 rounded-full bg-accent px-5 text-sm font-semibold hover:bg-accent-hover">Retry</button>
            </div>
          )}

          {q.isSuccess && items.length === 0 && (
            <div className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
              <p className="font-semibold">No results found</p>
              <p className="max-w-sm text-sm text-fg-2">Try different keywords{filtersActive ? " or remove some filters" : ""}.</p>
              {filtersActive && (
                <button type="button" onClick={() => router.replace(`/search?q=${encodeURIComponent(params.q)}`, { scroll: false })}
                  className="h-10 rounded-full border border-line bg-surface px-5 text-sm font-semibold hover:bg-line">Clear filters</button>
              )}
            </div>
          )}

          {items.length > 0 && (
            <ul className={`space-y-5 transition-opacity ${q.isPlaceholderData ? "opacity-60" : ""}`} aria-busy={q.isPlaceholderData}>
              {items.map((r) => (
                <li key={r.kind === "video" ? `v-${r.video.id}` : r.kind === "channel" ? `c-${r.channel.id}` : `p-${r.playlist.id}`}>
                  {r.kind === "video" ? <VideoRow video={r.video} /> : r.kind === "channel" ? <ChannelRow channel={r.channel} /> : <PlaylistRow playlist={r.playlist} />}
                </li>
              ))}
            </ul>
          )}

          {q.hasNextPage && (
            <button type="button" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}
              className="mx-auto mt-8 block h-11 rounded-full border border-line bg-surface px-8 text-sm font-semibold hover:bg-line disabled:opacity-60">
              {q.isFetchingNextPage ? "Loading…" : "Load more results"}
            </button>
          )}
        </>
      )}
    </div>
  );
}
