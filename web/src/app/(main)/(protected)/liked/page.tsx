import type { Metadata } from "next";
import { LikedView } from "@/features/library/LikedView";

export const metadata: Metadata = { title: "Liked videos", robots: { index: false, follow: false } };

export default function Page() {
  return <LikedView />;
}
