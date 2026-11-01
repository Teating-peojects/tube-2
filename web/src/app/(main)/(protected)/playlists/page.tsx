import type { Metadata } from "next";
import { PlaylistsView } from "@/features/playlists/PlaylistsView";

export const metadata: Metadata = { title: "Playlists", robots: { index: false, follow: false } };

export default function Page() {
  return <PlaylistsView />;
}
