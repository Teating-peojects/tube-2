import type { Metadata } from "next";
import { TrendingView } from "@/features/trending/TrendingView";

export const metadata: Metadata = {
  title: "Trending",
  description: "What’s trending on REDXTUBE right now.",
  alternates: { canonical: "/trending" },
};

export default function TrendingPage() {
  return <TrendingView />;
}
