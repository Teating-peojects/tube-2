"use client";
import { Check, ChatCircle, Trash } from "@phosphor-icons/react";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { EmptyState } from "@/components/ui/EmptyState";
import { Tabs } from "@/components/ui/Tabs";
import { CommentForm } from "@/features/comments/CommentForm";
import { errMsg } from "@/features/comments/CommentItem";
import { PagedList } from "@/features/library/PagedList";
import { createComment } from "@/services/comments";
import { approveComment, deleteComment, getStudioComments } from "@/services/studio";
import type { Paginated, StudioComment } from "@/types";
import { cn, timeAgo } from "@/utils/format";

type Data = InfiniteData<Paginated<StudioComment>>;
type Filter = "all" | "held";
const KEY = ["studio", "comments"] as const;
const tabs: { id: Filter; label: string }[] = [{ id: "all", label: "All comments" }, { id: "held", label: "Held for review" }];
const act = "inline-flex h-9 items-center gap-1.5 rounded-full px-3 text-xs font-semibold text-fg-2 transition-colors hover:bg-surface hover:text-white";

function Row({ c, onApprove, onDelete }: { c: StudioComment; onApprove: () => void; onDelete: () => void }) {
  const [replying, setReplying] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const reply = useMutation({ mutationFn: (text: string) => createComment(c.videoId, text, c.id) });
  return (
    <div className="space-y-2 p-4">
      <p className="text-xs text-fg-2">
        <span className="font-semibold text-white">{c.author}</span> • {timeAgo(c.createdAt)} on{" "}
        <Link href={`/watch/${c.videoId}`} className="underline hover:text-white">{c.videoTitle}</Link>
        {c.status === "held" && <span className={cn("ml-2 rounded-full bg-surface px-2 py-0.5 font-semibold text-white")}>Held for review</span>}
      </p>
      <p className="whitespace-pre-wrap break-words text-sm">{c.text}</p>
      <div className="-ml-3 flex flex-wrap items-center">
        {c.status === "held" && <button type="button" onClick={onApprove} className={act}><Check size={14} aria-hidden="true" />Approve</button>}
        <button type="button" onClick={() => setReplying((r) => !r)} aria-expanded={replying} className={act}><ChatCircle size={14} aria-hidden="true" />Reply</button>
        {confirm ? (
          <span role="group" aria-label="Confirm delete comment" className="flex items-center gap-1 text-xs">
            <button type="button" onClick={() => setConfirm(false)} className={act}>Cancel</button>
            <button type="button" onClick={onDelete} className={cn(act, "text-accent")}>Delete</button>
          </span>
        ) : (
          <button type="button" onClick={() => setConfirm(true)} className={act}><Trash size={14} aria-hidden="true" />Delete</button>
        )}
      </div>
      {replying && (
        <div className="flex gap-3">
          <CommentForm autoFocus placeholder={`Reply to ${c.author}…`} submitLabel="Reply" pending={reply.isPending} error={errMsg(reply.error)}
            onSubmit={(t) => reply.mutateAsync(t)} onCancel={() => setReplying(false)} />
        </div>
      )}
    </div>
  );
}

export function CommentsView() {
  const qc = useQueryClient();
  const [filter, setFilter] = useState<Filter>("all");

  const optimistic = (fn: (items: StudioComment[]) => StudioComment[]) => async () => {
    await qc.cancelQueries({ queryKey: KEY });
    const prev = qc.getQueriesData<Data>({ queryKey: KEY });
    qc.setQueriesData<Data>({ queryKey: KEY }, (old) => old && { ...old, pages: old.pages.map((p) => ({ ...p, items: fn(p.items) })) });
    return { prev };
  };
  const rollback = (_e: unknown, _v: unknown, ctx?: { prev: [readonly unknown[], Data | undefined][] }) => ctx?.prev.forEach(([k, d]) => qc.setQueryData(k, d));
  const settle = () => qc.invalidateQueries({ queryKey: KEY });

  const approve = useMutation({
    mutationFn: approveComment,
    onMutate: (id) => optimistic((items) => items.map((i) => (i.id === id ? { ...i, status: "published" as const } : i)))(),
    onError: rollback, onSettled: settle,
  });
  const del = useMutation({
    mutationFn: deleteComment,
    onMutate: (id) => optimistic((items) => items.filter((i) => i.id !== id))(),
    onError: rollback, onSettled: settle,
  });

  return (
    <div className="space-y-4">
      <Tabs tabs={tabs} value={filter} onChange={setFilter} label="Filter comments" idPrefix="studio-comments" />
      <div role="tabpanel" id="studio-comments-panel" aria-labelledby={`studio-comments-tab-${filter}`}>
        {(approve.isError || del.isError) && <p role="alert" className="mb-3 text-sm text-accent">Couldn’t update that comment. Try again.</p>}
        <PagedList<StudioComment> layout="rows" queryKey={[...KEY, filter]} fetchPage={(c) => getStudioComments(filter, c)} getKey={(c) => c.id}
          errorLabel="Couldn’t load comments"
          empty={<EmptyState icon={<ChatCircle size={28} />} title={filter === "held" ? "Nothing to review" : "No comments yet"} text={filter === "held" ? "Comments held for review will appear here." : "Comments on your videos will appear here."} />}
          render={(c) => <Row c={c} onApprove={() => approve.mutate(c.id)} onDelete={() => del.mutate(c.id)} />} />
      </div>
    </div>
  );
}
