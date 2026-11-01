"use client";
import { BookmarkSimple } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useRequireAuth } from "@/features/auth/useRequireAuth";
import { useSession } from "@/features/auth/useSession";
import { useToggleWatchLater } from "@/features/library/useWatchLater";
import { createPlaylist, getMyPlaylists, setPlaylistVideo } from "@/services/playlists";
import { toast } from "@/store/toast";
import type { PlaylistWithMembership } from "@/types";

/** "Save" popover: Watch Later + the user's playlists + quick-create. */
export function AddToPlaylist({ videoId }: { videoId: string }) {
  const { status } = useSession();
  const requireAuth = useRequireAuth();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const wl = useToggleWatchLater(videoId);
  const key = ["playlists", "menu", videoId];

  const list = useQuery({ queryKey: key, queryFn: () => getMyPlaylists(videoId), enabled: open && status === "authenticated" });

  const toggle = useMutation({
    mutationFn: (v: { id: string; add: boolean }) => setPlaylistVideo(v.id, videoId, v.add),
    onMutate: async (v) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<PlaylistWithMembership[]>(key);
      qc.setQueryData<PlaylistWithMembership[]>(key, (d) => d?.map((p) => (p.id === v.id ? { ...p, contains: v.add } : p)));
      return { prev };
    },
    onError: (_e, _v, ctx) => { if (ctx?.prev) qc.setQueryData(key, ctx.prev); }, // rollback
    onSettled: () => qc.invalidateQueries({ queryKey: ["playlists"] }),
  });
  const create = useMutation({
    mutationFn: () => createPlaylist({ title: title.trim(), visibility: "private", videoId }),
    onSuccess: () => { setTitle(""); toast.success("Playlist created"); void qc.invalidateQueries({ queryKey: ["playlists"] }); },
  });

  useEffect(() => {
    if (!open) return;
    root.current?.querySelector<HTMLInputElement>("input")?.focus();
    const onDown = (e: MouseEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false); };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") { setOpen(false); trigger.current?.focus(); } };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => { document.removeEventListener("mousedown", onDown); document.removeEventListener("keydown", onKey); };
  }, [open]);

  function onTrigger() {
    if (!requireAuth("Log in to save videos to playlists.")) return;
    setOpen((o) => !o);
  }
  function onCreate(e: FormEvent) {
    e.preventDefault();
    if (title.trim()) create.mutate();
  }

  return (
    <div ref={root} className="relative">
      <button ref={trigger} type="button" onClick={onTrigger} aria-haspopup="dialog" aria-expanded={open}
        className="inline-flex h-10 items-center gap-2 rounded-full border border-line bg-surface px-4 text-sm font-semibold transition-colors hover:bg-line">
        <BookmarkSimple size={20} aria-hidden="true" />Save
      </button>
      {open && (
        <div role="dialog" aria-label="Save video to" className="fixed inset-x-3 bottom-20 z-40 rounded-xl border border-line bg-card p-3 shadow-soft sm:absolute sm:inset-auto sm:left-0 sm:top-12 sm:w-72">
          <p className="px-1 pb-2 text-[11px] font-semibold tracking-wider text-fg-3">SAVE TO</p>
          <ul className="max-h-56 space-y-0.5 overflow-y-auto">
            <li><label className="flex min-h-11 cursor-pointer items-center gap-3 rounded-lg px-2 text-sm hover:bg-surface">
              <input type="checkbox" checked={wl.saved} onChange={wl.toggle} className="size-5 accent-accent" />Watch Later
            </label></li>
            {list.isPending && <li className="skeleton h-10 rounded-lg" aria-hidden="true" />}
            {list.isError && <li role="alert" className="px-2 py-2 text-xs text-accent">Couldn’t load playlists.</li>}
            {list.data?.map((p) => (
              <li key={p.id}><label className="flex min-h-11 cursor-pointer items-center gap-3 rounded-lg px-2 text-sm hover:bg-surface">
                <input type="checkbox" checked={p.contains} onChange={(e) => toggle.mutate({ id: p.id, add: e.target.checked })} className="size-5 accent-accent" />
                <span className="truncate">{p.title}</span>
              </label></li>
            ))}
          </ul>
          <form onSubmit={onCreate} className="mt-2 flex gap-2 border-t border-line pt-3">
            <label htmlFor="np" className="sr-only">New playlist name</label>
            <input id="np" value={title} maxLength={100} onChange={(e) => setTitle(e.target.value)} placeholder="New playlist"
              className="h-10 min-w-0 flex-1 rounded-lg border border-line bg-bg px-3 text-sm outline-none focus:border-white" />
            <button type="submit" disabled={!title.trim() || create.isPending} className="h-10 rounded-lg bg-accent px-4 text-sm font-semibold hover:bg-accent-hover disabled:opacity-50">Create</button>
          </form>
          {(create.isError || toggle.isError) && <p role="alert" className="mt-2 text-xs text-accent">Something went wrong. Try again.</p>}
        </div>
      )}
    </div>
  );
}
