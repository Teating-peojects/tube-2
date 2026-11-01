import type { Metadata } from "next";
import { WatchLaterView } from "@/features/library/WatchLaterView";

export const metadata: Metadata = { title: "Watch later", robots: { index: false, follow: false } };

export default function Page() {
  return <WatchLaterView />;
}
