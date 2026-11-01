import type { Metadata } from "next";
import { OverviewView } from "@/features/studio/OverviewView";

export const metadata: Metadata = { title: "Overview · Studio", robots: { index: false, follow: false } };

export default function Page() {
  return <OverviewView />;
}
