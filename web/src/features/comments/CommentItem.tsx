"use client";
import { CaretDown, CaretUp, Flag, PushPin, ThumbsUp } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError } from "@/services/api";
import { createComment, getReplies, likeComment } from "@/services/comments";
import { modals } from "@/store/modal";
import type { Comment } from "@/types";
import { cn, formatViews, timeAgo } from "@/utils/format";
import { CommentForm } from "./CommentForm";

export function errMsg(e: unknown): string | null {
  if (!e) return null;
  if (e instanceof ApiError && e.status === 401) return "Log in to comment.";
  if (e instanceof ApiError && e.status === 429) return "You’re commenting too fast. Try again shortly.";
  return "Couldn’t post your comment. Try again.";
}

export function CommentItem({ comment: c, videoId, isReply = false }: { comment: Comment; videoId: string; isReply?: boolean }) {
  const qc = useQueryClient();
  const temp = c.id.startsWith("tmp-");
  const [liked, setLiked] = useState(c.liked);
  const [likes, setLikes] = useState(c.likes);
  const [replying, setReplying] = useState(false);
  const [open, setOpen] = useState(false);
  const [extra, setExtra] = useState(0);

  const like = useMutation({
    mutationFn: (next: boolean) => likeComment(c.id, next),
    onMutate: (next) => { setLiked(next); setLikes((n) => n + (next ? 1 : -1)); },
    onError: (_e, next) => { setLiked(!next); setLikes((n) => n + (next ? -1 : 1)); }, // rollback
  });

  const replies = useQuery({ queryKey: ["replies", c.id], queryFn: () => getReplies(c.id), enabled: open });
  const reply = useMutation({
    mutationFn: (text: string) => createComment(videoId, text, c.id),
    onSuccess: () => { setExtra((n) => n + 1); setOpen(true); void qc.invalidateQueries({ queryKey: ["replies", c.id] }); },
  });
  const total = c.replyCount + extra;

  return (
    <li className="flex gap-3" aria-busy={temp}>
      <span aria-hidden="true" className={cn("grid shrink-0 place-items-center rounded-full border border-line bg-surface text-xs font-bold", isReply ? "size-7" : "size-9")}>
        {c.author.name.charAt(0).toUpperCase()}
      </span>
      <div className="min-w-0 flex-1">
        {c.pinned && <p className="mb-0.5 flex items-center gap-1 text-[11px] font-semibold text-fg-2"><PushPin size={12} weight="fill" aria-hidden="true" />Pinned</p>}
        <p className="text-xs text-fg-2">
          <span className={cn("font-semibold text-white", c.author.isCreator && "rounded-full bg-surface px-2 py-0.5")}>{c.author.name}</span>
          {c.author.isCreator && <span className="ml-1.5 rounded bg-accent px-1.5 py-0.5 text-[10px] font-bold text-white">CREATOR</span>}
          <span className="ml-2">{timeAgo(c.createdAt)}</span>
        </p>
        {/* Plain text only: escaped by React, never injected as HTML. */}
        <p className="mt-1 whitespace-pre-wrap break-words text-sm">{c.text}</p>
        <div className="mt-1 flex items-center gap-1 text-xs text-fg-2">
          <button type="button" disabled={temp || like.isPending} aria-pressed={liked} aria-label={`Like, ${likes} likes`} onClick={() => like.mutate(!liked)}
            className={cn("flex h-9 items-center gap-1.5 rounded-full px-2.5 hover:bg-surface disabled:opacity-60", liked && "text-accent")}>
            <ThumbsUp size={16} weight={liked ? "fill" : "regular"} aria-hidden="true" />{likes > 0 && formatViews(likes)}
          </button>
          {!isReply && <button type="button" disabled={temp} onClick={() => setReplying((r) => !r)} className="h-9 rounded-full px-3 font-semibold hover:bg-surface">Reply</button>}
          <button type="button" onClick={() => modals.report({ type: "comment", id: c.id })} aria-label="Report comment" className="grid size-9 place-items-center rounded-full hover:bg-surface"><Flag size={16} aria-hidden="true" /></button>
        </div>

        {replying && (
          <div className="mt-2 flex gap-3">
            <CommentForm autoFocus placeholder={`Reply to ${c.author.name}…`} submitLabel="Reply" pending={reply.isPending} error={errMsg(reply.error)}
              onSubmit={(t) => reply.mutateAsync(t)} onCancel={() => setReplying(false)} />
          </div>
        )}

        {!isReply && total > 0 && (
          <>
            <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)}
              className="mt-1 flex h-9 items-center gap-1.5 rounded-full px-2.5 text-sm font-semibold text-accent hover:bg-surface">
              {open ? <CaretUp size={14} aria-hidden="true" /> : <CaretDown size={14} aria-hidden="true" />}
              {total} {total === 1 ? "reply" : "replies"}
            </button>
            {open && (
              <ul className="mt-2 space-y-4">
                {replies.isPending && <li className="skeleton h-12 rounded-lg" aria-hidden="true" />}
                {replies.isError && <li role="alert" className="text-xs text-accent">Couldn’t load replies.</li>}
                {replies.data?.items.map((r) => <CommentItem key={r.id} comment={r} videoId={videoId} isReply />)}
              </ul>
            )}
          </>
        )}
      </div>
    </li>
  );
}
