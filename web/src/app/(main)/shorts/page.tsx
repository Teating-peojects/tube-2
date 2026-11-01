import type { Metadata } from "next";
import { ShortsFeed } from "@/features/shorts/ShortsFeed";

export const metadata: Metadata = { title: "Shorts", description: "Short videos on REDXTUBE.", alternates: { canonical: "/shorts" } };

export default function Page() {
  return <ShortsFeed />;
}
