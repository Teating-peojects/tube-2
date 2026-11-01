import type { Metadata } from "next";
import { CategoryTiles } from "@/features/explore/CategoryTiles";
import { VideoFeed } from "@/features/videos/VideoFeed";
import { categories } from "@/config/nav";

type Props = { searchParams: Promise<{ category?: string | string[] }> };

export const metadata: Metadata = {
  title: "Explore",
  description: "Browse REDXTUBE by category.",
  alternates: { canonical: "/explore" },
};

export default async function ExplorePage({ searchParams }: Props) {
  const { category } = await searchParams;
  // URL input is validated against the known category list.
  const active = typeof category === "string" && category !== "All" && categories.includes(category) ? category : "";

  return (
    <div className="space-y-8">
      <h1 className="text-2xl font-extrabold">Explore</h1>
      <CategoryTiles active={active} />
      <section aria-labelledby="feed-h">
        <h2 id="feed-h" className="mb-4 text-lg font-bold">{active ? active : "Popular right now"}</h2>
        <VideoFeed category={active || "All"} showChips={false} />
      </section>
    </div>
  );
}
