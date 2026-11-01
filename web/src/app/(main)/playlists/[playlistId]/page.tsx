import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { cache } from "react";
import { PlaylistDetailView } from "@/features/playlists/PlaylistDetailView";
import { serverInit } from "@/lib/server-fetch";
import { ApiError } from "@/services/api";
import { getPlaylist } from "@/services/playlists";

type Props = { params: Promise<{ playlistId: string }> };

const load = cache(async (id: string) => {
  try {
    return await getPlaylist(id, await serverInit());
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 403)) return null; // private/missing look the same
    throw e;
  }
});

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { playlistId } = await params;
  const p = await load(playlistId);
  if (!p) return { title: "Playlist unavailable", robots: { index: false } };
  // Only public playlists are indexable; nothing private leaks into metadata.
  return p.visibility === "public"
    ? { title: p.title, description: `${p.videoCount} videos by ${p.ownerName}`, alternates: { canonical: `/playlists/${p.id}` } }
    : { title: "Playlist", robots: { index: false, follow: false } };
}

export default async function PlaylistPage({ params }: Props) {
  const { playlistId } = await params;
  const p = await load(playlistId);
  if (!p) notFound();
  return <PlaylistDetailView playlistId={p.id} initial={p} />;
}
