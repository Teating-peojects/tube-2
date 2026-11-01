import type { Metadata } from "next";
import { HistoryView } from "@/features/library/HistoryView";

export const metadata: Metadata = { title: "Watch history", robots: { index: false, follow: false } };

export default function Page() {
  return <HistoryView />;
}
