import type { Metadata } from "next";
import { LiveView } from "@/features/shorts/LiveView";

export const metadata: Metadata = { title: "Live", description: "Live streams on REDXTUBE.", alternates: { canonical: "/live" } };

export default function Page() {
  return <LiveView />;
}
