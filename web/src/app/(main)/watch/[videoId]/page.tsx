import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { cache } from "react";
import { WatchView } from "@/features/videos/WatchView";
import { ApiError } from "@/services/api";
import { serverInit } from "@/lib/server-fetch";
import { getVideo } from "@/services/videos";

type Props = { params: Promise<{ videoId: string }> };

// Deduplicates the fetch between generateMetadata and the page.
const load = cache(async (id: string) => {
  try {
    return await getVideo(id, await serverInit());
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
});

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { videoId } = await params;
  const v = await load(videoId);
  if (!v) return { title: "Video unavailable", robots: { index: false } };
  const description = v.description.slice(0, 160);
  return {
    title: v.title,
    description,
    alternates: { canonical: `/watch/${v.id}` },
    openGraph: { type: "video.other", title: v.title, description, url: `/watch/${v.id}`, images: v.posterUrl ? [v.posterUrl] : undefined },
    twitter: { card: v.posterUrl ? "summary_large_image" : "summary", title: v.title, description },
  };
}

export default async function WatchPage({ params }: Props) {
  const { videoId } = await params;
  const video = await load(videoId);
  if (!video) notFound();

  // Structured data only when a thumbnail exists; no private user data included.
  const jsonLd = video.posterUrl && {
    "@context": "https://schema.org", "@type": "VideoObject",
    name: video.title, description: video.description.slice(0, 300),
    thumbnailUrl: [video.posterUrl], uploadDate: video.publishedAt, duration: `PT${video.durationSec}S`,
  };

  return (
    <>
      {jsonLd && <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd).replace(/</g, "\\u003c") }} />}
      <WatchView videoId={video.id} initial={video} />
    </>
  );
}
