import type { Metadata } from "next";
import { VideosView } from "@/features/studio/VideosView";

export const metadata: Metadata = { title: "Videos · Studio", robots: { index: false, follow: false } };

export default function Page() {
  return <VideosView />;
}
