"use client";
import { ListPlus, Plus } from "@phosphor-icons/react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { Input } from "@/components/ui/Input";
import { playlistSchema, type PlaylistValues } from "@/features/auth/schemas";
import { videoGridCls } from "@/features/videos/grid";
import { createPlaylist, getMyPlaylists } from "@/services/playlists";
import { PlaylistCard } from "./PlaylistCard";

export function PlaylistsView() {
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);
  const q = useQuery({ queryKey: ["playlists", "mine"], queryFn: () => getMyPlaylists() });
  const { register, handleSubmit, reset, formState: { errors } } = useForm<PlaylistValues>({
    resolver: zodResolver(playlistSchema), defaultValues: { title: "", visibility: "private" },
  });
  const create = useMutation({
    mutationFn: (v: PlaylistValues) => createPlaylist(v),
    onSuccess: () => { reset(); setCreating(false); void qc.invalidateQueries({ queryKey: ["playlists"] }); },
  });

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-extrabold">Playlists</h1>
        <Button onClick={() => setCreating((c) => !c)} aria-expanded={creating}><Plus size={16} aria-hidden="true" />New playlist</Button>
      </div>

      {creating && (
        <form onSubmit={handleSubmit((v) => create.mutate(v))} noValidate className="grid gap-4 rounded-card border border-line bg-card p-5 sm:grid-cols-[1fr_180px_auto] sm:items-start">
          <Input label="Name" maxLength={100} autoFocus error={errors.title?.message} {...register("title")} />
          <div>
            <label htmlFor="vis" className="mb-1.5 block text-sm font-medium">Visibility</label>
            <select id="vis" className="h-12 w-full rounded-xl border border-line bg-card px-3 text-sm" {...register("visibility")}>
              <option value="private">Private</option>
              <option value="public">Public</option>
            </select>
          </div>
          <Button type="submit" className="sm:mt-7" loading={create.isPending}>Create</Button>
          {create.isError && <p role="alert" className="text-xs text-accent sm:col-span-3">Couldn’t create the playlist. Try again.</p>}
        </form>
      )}

      {q.isPending && <div className={videoGridCls} aria-hidden="true">{Array.from({ length: 4 }, (_, i) => <div key={i} className="skeleton aspect-video rounded-card" />)}</div>}
      {q.isError && (
        <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
          <p className="font-semibold">Couldn’t load your playlists</p>
          <Button onClick={() => q.refetch()}>Retry</Button>
        </div>
      )}
      {q.isSuccess && q.data.length === 0 && !creating && (
        <EmptyState icon={<ListPlus size={28} />} title="No playlists yet" text="Create a playlist to organize the videos you love." />
      )}
      {q.isSuccess && q.data.length > 0 && (
        <ul className={videoGridCls}>{q.data.map((p) => <li key={p.id}><PlaylistCard playlist={p} /></li>)}</ul>
      )}
    </div>
  );
}
