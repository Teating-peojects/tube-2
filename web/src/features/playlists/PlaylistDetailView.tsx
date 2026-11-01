"use client";
import { ArrowDown, ArrowUp, Lock, Play, Trash, X } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { VideoRow } from "@/features/videos/VideoRow";
import { deletePlaylist, getPlaylist, reorderPlaylist, setPlaylistVideo, updatePlaylist } from "@/services/playlists";
import type { PlaylistDetail, Visibility } from "@/types";
import { timeAgo } from "@/utils/format";

const iconBtn = "grid size-9 place-items-center rounded-full text-fg-2 transition-colors hover:bg-surface hover:text-white disabled:opacity-40";

export function PlaylistDetailView({ playlistId, initial }: { playlistId: string; initial: PlaylistDetail }) {
  const qc = useQueryClient();
  const router = useRouter();
  const key = ["playlist", playlistId];
  const { data: p } = useQuery({ queryKey: key, queryFn: () => getPlaylist(playlistId), initialData: initial });

  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(p.title);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Optimistic helper with rollback.
  const optimistic = (fn: (d: PlaylistDetail) => PlaylistDetail) => ({
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<PlaylistDetail>(key);
      qc.setQueryData<PlaylistDetail>(key, (d) => (d ? fn(d) : d));
      return { prev };
    },
    onError: (_e: unknown, _v: unknown, ctx?: { prev?: PlaylistDetail }) => { if (ctx?.prev) qc.setQueryData(key, ctx.prev); },
    onSettled: () => qc.invalidateQueries({ queryKey: ["playlists"] }),
  });

  const update = useMutation({
    mutationFn: (patch: { title?: string; visibility?: Visibility }) => updatePlaylist(playlistId, patch),
    onMutate: async (patch) => optimistic((d) => ({ ...d, ...patch })).onMutate(),
    onError: (e, v, ctx) => optimistic((d) => d).onError(e, v, ctx),
    onSettled: () => qc.invalidateQueries({ queryKey: ["playlists"] }),
  });
  const removeVideo = useMutation({
    mutationFn: (videoId: string) => setPlaylistVideo(playlistId, videoId, false),
    onMutate: async (videoId) => optimistic((d) => ({ ...d, videos: d.videos.filter((v) => v.id !== videoId), videoCount: d.videoCount - 1 })).onMutate(),
    onError: (e, v, ctx) => optimistic((d) => d).onError(e, v, ctx),
    onSettled: () => qc.invalidateQueries({ queryKey: ["playlists"] }),
  });
  const reorder = useMutation({
    mutationFn: (ids: string[]) => reorderPlaylist(playlistId, ids),
    onMutate: async (ids) => optimistic((d) => ({ ...d, videos: ids.map((id) => d.videos.find((v) => v.id === id)!).filter(Boolean) })).onMutate(),
    onError: (e, v, ctx) => optimistic((d) => d).onError(e, v, ctx),
  });
  const del = useMutation({
    mutationFn: () => deletePlaylist(playlistId),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["playlists"] }); router.replace("/playlists"); },
  });

  function move(i: number, dir: -1 | 1) {
    const ids = p.videos.map((v) => v.id);
    const j = i + dir;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j]!, ids[i]!];
    reorder.mutate(ids);
  }
  function saveTitle() {
    const t = title.trim();
    if (!t || t.length > 100) return;
    update.mutate({ title: t });
    setEditing(false);
  }

  const first = p.videos[0];
  const failed = update.isError || removeVideo.isError || reorder.isError || del.isError;

  return (
    <div className="mx-auto max-w-5xl">
      <header className="mb-6 space-y-3 rounded-card border border-line bg-card p-5">
        {editing ? (
          <form onSubmit={(e) => { e.preventDefault(); saveTitle(); }} className="flex flex-wrap items-center gap-2">
            <label htmlFor="pl-title" className="sr-only">Playlist name</label>
            <input id="pl-title" value={title} maxLength={100} autoFocus onChange={(e) => setTitle(e.target.value)}
              className="h-11 min-w-0 flex-1 rounded-xl border border-line bg-bg px-4 text-lg font-bold outline-none focus:border-white" />
            <Button type="submit" className="h-10" disabled={!title.trim()}>Save</Button>
            <Button variant="secondary" className="h-10" onClick={() => { setTitle(p.title); setEditing(false); }}>Cancel</Button>
          </form>
        ) : (
          <h1 className="flex items-center gap-2 text-2xl font-extrabold">
            {p.title}
            {p.visibility === "private" && <Lock size={18} className="text-fg-2" aria-label="Private" />}
          </h1>
        )}
        <p className="text-sm text-fg-2">{p.ownerName} • {p.videoCount} {p.videoCount === 1 ? "video" : "videos"} • Updated {timeAgo(p.updatedAt)}</p>
        {p.description && <p className="whitespace-pre-line text-sm text-fg-2">{p.description}</p>}

        <div className="flex flex-wrap items-center gap-2 pt-1">
          {first && <Link href={`/watch/${first.id}`} className="inline-flex h-10 items-center gap-2 rounded-full bg-accent px-5 text-sm font-semibold hover:bg-accent-hover"><Play size={16} weight="fill" aria-hidden="true" />Play all</Link>}
          {p.isOwner && (
            <>
              {!editing && <Button variant="secondary" className="h-10" onClick={() => { setTitle(p.title); setEditing(true); }}>Rename</Button>}
              <label className="flex items-center gap-2 text-sm text-fg-2">
                Visibility
                <select value={p.visibility} onChange={(e) => update.mutate({ visibility: e.target.value as Visibility })}
                  className="h-10 rounded-lg border border-line bg-card px-2.5 text-sm text-white">
                  <option value="private">Private</option>
                  <option value="public">Public</option>
                </select>
              </label>
              {confirmDelete ? (
                <span role="group" aria-label="Confirm delete" className="flex items-center gap-2 text-sm">
                  <span className="text-fg-2">Delete this playlist?</span>
                  <Button variant="secondary" className="h-9 px-4" onClick={() => setConfirmDelete(false)}>Cancel</Button>
                  <Button className="h-9 px-4" loading={del.isPending} onClick={() => del.mutate()}>Delete</Button>
                </span>
              ) : (
                <Button variant="ghost" className="h-10" onClick={() => setConfirmDelete(true)}><Trash size={16} aria-hidden="true" />Delete</Button>
              )}
            </>
          )}
        </div>
        {failed && <p role="alert" className="text-xs text-accent">Something didn’t save. Please try again.</p>}
      </header>

      {p.videos.length === 0 ? (
        <EmptyState icon={<Play size={28} />} title="This playlist is empty" text="Use “Save” on any video to add it here." action={{ label: "Browse videos", href: "/" }} />
      ) : (
        <ol className="space-y-5">
          {p.videos.map((v, i) => (
            <li key={v.id} className="flex items-start gap-2">
              <span aria-hidden="true" className="mt-1 hidden w-6 shrink-0 text-right text-sm tabular-nums text-fg-3 sm:block">{i + 1}</span>
              <div className="min-w-0 flex-1"><VideoRow video={v} /></div>
              {p.isOwner && (
                <div className="flex shrink-0 flex-col" role="group" aria-label={`Manage ${v.title}`}>
                  <button type="button" className={iconBtn} disabled={i === 0 || reorder.isPending} onClick={() => move(i, -1)} aria-label={`Move ${v.title} up`}><ArrowUp size={16} /></button>
                  <button type="button" className={iconBtn} disabled={i === p.videos.length - 1 || reorder.isPending} onClick={() => move(i, 1)} aria-label={`Move ${v.title} down`}><ArrowDown size={16} /></button>
                  <button type="button" className={iconBtn} onClick={() => removeVideo.mutate(v.id)} aria-label={`Remove ${v.title} from playlist`}><X size={16} /></button>
                </div>
              )}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
