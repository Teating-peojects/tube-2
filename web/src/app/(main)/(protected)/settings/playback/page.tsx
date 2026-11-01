import type { Metadata } from "next";
import { PlaybackPanel } from "@/features/settings/panels";

export const metadata: Metadata = { title: "Playback settings", robots: { index: false, follow: false } };

export default function Page() {
  return <PlaybackPanel />;
}
