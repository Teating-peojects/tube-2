import type { Metadata } from "next";
import { AnalyticsView } from "@/features/studio/AnalyticsView";

export const metadata: Metadata = { title: "Analytics · Studio", robots: { index: false, follow: false } };

export default function Page() {
  return <AnalyticsView />;
}
