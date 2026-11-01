import type { Paginated, Video } from "@/types";

const titles = [
  "Neon City Nights: A Cinematic Drive", "Behind the Scenes: Studio Session", "Live Q&A with the Community",
  "Short Film: The Last Train", "Top 10 Moments of the Month", "Full Length Feature Presentation",
  "Montage: Best of the Week", "Interview Series: Episode 12",
];
const creators = ["Creator Name", "Studio Name", "Channel Name"];

export const all: Video[] = Array.from({ length: 48 }, (_, i) => ({
  id: `v${i + 1}`,
  title: titles[i % titles.length] ?? "Untitled",
  thumbnailUrl: null,
  durationSec: 360 + ((i * 397) % 2400),
  quality: i % 5 === 0 ? "4K" : "HD",
  views: 12000 + ((i * 7919) % 3000000),
  publishedAt: new Date(Date.now() - (i + 1) * 86400000 * 1.7).toISOString(),
  isLive: i % 11 === 2,
  creator: { id: `c${(i % 3) + 1}`, name: creators[i % 3] ?? "Creator", avatarUrl: null, verified: i % 2 === 0 },
}));

const PAGE = 12;

export async function mockGetVideos(p: { cursor?: string }): Promise<Paginated<Video>> {
  await new Promise((r) => setTimeout(r, 400));
  const start = p.cursor ? Number(p.cursor) : 0;
  const items = all.slice(start, start + PAGE);
  const next = start + PAGE;
  return { items, nextCursor: next < all.length ? String(next) : null };
}
