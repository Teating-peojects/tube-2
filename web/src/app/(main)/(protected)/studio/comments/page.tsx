import type { Metadata } from "next";
import { CommentsView } from "@/features/studio/CommentsView";

export const metadata: Metadata = { title: "Comments · Studio", robots: { index: false, follow: false } };

export default function Page() {
  return <CommentsView />;
}
