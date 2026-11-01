"use client";
import { useInfiniteQuery, useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { createComment, getComments } from "@/services/comments";
import { toast } from "@/store/toast";
import type { Comment, CommentSort, Paginated } from "@/types";
import { CommentForm } from "./CommentForm";
import { CommentItem, errMsg } from "./CommentItem";

type Pages = InfiniteData<Paginated<Comment>>;

export function CommentSection({ videoId, total }: { videoId: string; total: number }) {
  const qc = useQueryClient();
  const [sort, setSort] = useState<CommentSort>("top");
  const [visible, setVisible] = useState(false);
  const ref = useRef<HTMLElement>(null);
  const key = ["comments", videoId, sort];

  // Lazy load: nothing is fetched until the section approaches the viewport.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(([e]) => { if (e?.isIntersecting) { setVisible(true); io.disconnect(); } }, { rootMargin: "300px" });
    io.observe(el);
    return () => io.disconnect();
  }, []);

  const q = useInfiniteQuery({
    queryKey: key,
    queryFn: ({ pageParam }) => getComments(videoId, { cursor: pageParam, sort }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: visible,
  });

  const create = useMutation({
    mutationFn: (text: string) => createComment(videoId, text),
    onMutate: async (text) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Pages>(key);
      const temp: Comment = {
        id: `tmp-${Date.now()}`, author: { id: "me", name: "You", avatarUrl: null, isCreator: false },
        text, createdAt: new Date().toISOString(), likes: 0, liked: false, replyCount: 0, pinned: false,
      };
      qc.setQueryData<Pages>(key, (old) => old && { ...old, pages: old.pages.map((p, i) => (i === 0 ? { ...p, items: [temp, ...p.items] } : p)) });
      return { prev };
    },
    onSuccess: () => toast.success("Comment posted"),
    onError: (_e, _t, ctx) => { if (ctx?.prev) qc.setQueryData(key, ctx.prev); }, // rollback
    onSettled: () => qc.invalidateQueries({ queryKey: ["comments", videoId] }),
  });

  const items = q.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <section ref={ref} aria-labelledby="comments-h" className="pt-2">
      <div className="mb-4 flex items-center gap-4">
        <h2 id="comments-h" className="text-lg font-bold">{total.toLocaleString("en")} Comments</h2>
        <label className="ml-auto flex items-center gap-2 text-sm text-fg-2">
          Sort by
          <select value={sort} onChange={(e) => setSort(e.target.value as CommentSort)}
            className="h-9 rounded-lg border border-line bg-card px-2 text-sm text-white">
            <option value="top">Top</option>
            <option value="new">Newest</option>
          </select>
        </label>
      </div>

      <div className="mb-6 flex gap-3">
        <span aria-hidden="true" className="size-9 shrink-0 rounded-full border border-line bg-surface" />
        <CommentForm pending={create.isPending} error={errMsg(create.error)} onSubmit={(t) => create.mutateAsync(t)} />
      </div>

      {!visible || q.isPending ? (
        <ul className="space-y-5" aria-hidden="true">
          {Array.from({ length: 4 }, (_, i) => (
            <li key={i} className="flex gap-3"><div className="skeleton size-9 rounded-full" /><div className="flex-1 space-y-2"><div className="skeleton h-3 w-1/4 rounded" /><div className="skeleton h-3.5 w-3/4 rounded" /></div></li>
          ))}
        </ul>
      ) : q.isError ? (
        <div role="alert" className="rounded-card border border-line bg-card p-4 text-sm">
          Couldn’t load comments.{" "}
          <button type="button" onClick={() => q.refetch()} className="font-semibold text-accent hover:underline">Retry</button>
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-card border border-line bg-card p-6 text-center text-sm text-fg-2">No comments yet. Be the first to comment.</p>
      ) : (
        <ul className="space-y-5">{items.map((c) => <CommentItem key={c.id} comment={c} videoId={videoId} />)}</ul>
      )}

      {q.hasNextPage && (
        <button type="button" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}
          className="mx-auto mt-6 block h-10 rounded-full border border-line bg-surface px-6 text-sm font-semibold hover:bg-line disabled:opacity-60">
          {q.isFetchingNextPage ? "Loading…" : "Show more comments"}
        </button>
      )}
    </section>
  );
}
