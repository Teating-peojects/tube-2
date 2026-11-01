"use client";
import { useInfiniteQuery, type QueryKey } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { VideoCardSkeleton } from "@/features/videos/VideoCard";
import { videoGridCls } from "@/features/videos/grid";
import { VideoRowSkeleton } from "@/features/videos/VideoRow";
import type { Paginated } from "@/types";

type Props<T> = {
  queryKey: QueryKey;
  fetchPage: (cursor?: string) => Promise<Paginated<T>>;
  getKey: (item: T) => string;
  render: (item: T, index: number) => ReactNode;
  empty: ReactNode;
  layout?: "list" | "grid" | "rows";
  errorLabel?: string;
};

/** Generic paginated list with skeleton / empty / error / retry / load-more states. */
export function PagedList<T>({ queryKey, fetchPage, getKey, render, empty, layout = "list", errorLabel = "Couldn’t load this list" }: Props<T>) {
  const q = useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam }) => fetchPage(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  const cls = layout === "grid" ? videoGridCls : layout === "rows" ? "divide-y divide-line overflow-hidden rounded-card border border-line bg-card" : "space-y-5";

  if (q.isPending) return <div className={cls} aria-hidden="true">{Array.from({ length: layout === "grid" ? 8 : 5 }, (_, i) => (layout === "grid" ? <VideoCardSkeleton key={i} /> : layout === "rows" ? <div key={i} className="skeleton h-24 rounded-none" /> : <VideoRowSkeleton key={i} />))}</div>;
  if (q.isError) {
    return (
      <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
        <p className="font-semibold">{errorLabel}</p>
        <button type="button" onClick={() => q.refetch()} className="h-10 rounded-full bg-accent px-5 text-sm font-semibold hover:bg-accent-hover">Retry</button>
      </div>
    );
  }
  if (items.length === 0) return <>{empty}</>;

  return (
    <>
      <ul className={cls}>{items.map((it, i) => <li key={getKey(it)}>{render(it, i)}</li>)}</ul>
      {q.hasNextPage && (
        <button type="button" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}
          className="mx-auto mt-8 block h-11 rounded-full border border-line bg-surface px-8 text-sm font-semibold hover:bg-line disabled:opacity-60">
          {q.isFetchingNextPage ? "Loading…" : "Show more"}
        </button>
      )}
    </>
  );
}
