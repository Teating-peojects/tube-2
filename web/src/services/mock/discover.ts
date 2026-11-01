import type { ChannelResult, PlaylistSummary, SearchPage, SearchParams, SearchResult, TrendingTab, Video, Paginated } from "@/types";
import { all } from "./data";

const wait = (ms = 300) => new Promise((r) => setTimeout(r, ms));
const DAY = 86400000;
const PAGE = 10;

const names = ["Creator Name", "Studio Name", "Channel Name", "Neon Studio", "Night Frames", "Cinema Lab", "Studio Nine", "Frame Works"];
export const channels: ChannelResult[] = names.map((name, i) => ({
  id: `c${i + 1}`, name, avatarUrl: null, verified: i % 2 === 0,
  subscribers: 50000 + i * 37123, isSubscribed: false, videoCount: 20 + i * 7,
  description: "Original productions, behind-the-scenes and weekly releases.",
}));

export const playlists: PlaylistSummary[] = [
  { id: "p1", title: "Best of Neon City", videoCount: 14, thumbnailUrl: null, creatorName: "Creator Name" },
  { id: "p2", title: "Studio Sessions", videoCount: 9, thumbnailUrl: null, creatorName: "Studio Name" },
  { id: "p3", title: "Live Highlights", videoCount: 22, thumbnailUrl: null, creatorName: "Channel Name" },
  { id: "p4", title: "Weekly Montages", videoCount: 31, thumbnailUrl: null, creatorName: "Neon Studio" },
];

const ageDays = (v: Video) => Math.max(0.5, (Date.now() - +new Date(v.publishedAt)) / DAY);

export async function mockSearch(p: SearchParams & { cursor?: string }): Promise<SearchPage> {
  await wait(350);
  const n = p.q.trim().toLowerCase();
  const match = (s: string) => s.toLowerCase().includes(n);

  let vids = all.filter((v) => match(v.title) || match(v.creator.name));
  if (p.type === "live") vids = vids.filter((v) => v.isLive);
  if (p.duration === "short") vids = vids.filter((v) => v.durationSec < 240);
  if (p.duration === "medium") vids = vids.filter((v) => v.durationSec >= 240 && v.durationSec <= 1200);
  if (p.duration === "long") vids = vids.filter((v) => v.durationSec > 1200);
  const span = { today: DAY, week: 7 * DAY, month: 30 * DAY, year: 365 * DAY }[p.date as "today"];
  if (p.date !== "any" && span) vids = vids.filter((v) => Date.now() - +new Date(v.publishedAt) <= span);

  vids.sort((a, b) => {
    if (p.sort === "latest") return +new Date(b.publishedAt) - +new Date(a.publishedAt);
    if (p.sort === "views") return b.views - a.views;
    const ai = a.title.toLowerCase().indexOf(n), bi = b.title.toLowerCase().indexOf(n); // relevance
    return (ai < 0 ? 999 : ai) - (bi < 0 ? 999 : bi) || b.views - a.views;
  });

  const showVideos = p.type !== "channels" && p.type !== "playlists";
  const chs = p.type === "all" || p.type === "channels" ? channels.filter((c) => match(c.name)).slice(0, p.type === "channels" ? 99 : 2) : [];
  const pls = p.type === "all" || p.type === "playlists" ? playlists.filter((x) => match(x.title) || match(x.creatorName)).slice(0, p.type === "playlists" ? 99 : 1) : [];

  const list: SearchResult[] = [
    ...chs.map((channel) => ({ kind: "channel" as const, channel })),
    ...pls.map((playlist) => ({ kind: "playlist" as const, playlist })),
    ...(showVideos ? vids.map((video) => ({ kind: "video" as const, video })) : []),
  ];
  const start = p.cursor ? Number(p.cursor) : 0;
  const next = start + PAGE;
  return { items: list.slice(start, next), nextCursor: next < list.length ? String(next) : null, total: list.length };
}

export async function mockSuggest(q: string): Promise<string[]> {
  await wait(120);
  const n = q.trim().toLowerCase();
  const pool = [...new Set([...all.map((v) => v.title), ...channels.map((c) => c.name), ...playlists.map((x) => x.title)])];
  return pool.filter((s) => s.toLowerCase().includes(n)).slice(0, 6);
}

export async function mockTrending(tab: TrendingTab, cursor?: string): Promise<Paginated<Video>> {
  await wait(350);
  const byViews = [...all].sort((a, b) => b.views - a.views);
  let list: Video[];
  if (tab === "most-viewed") list = byViews;
  else if (tab === "fast-growing") list = [...all].sort((a, b) => b.views / ageDays(b) - a.views / ageDays(a));
  else if (tab === "recent") list = byViews.slice(0, 20).sort((a, b) => +new Date(b.publishedAt) - +new Date(a.publishedAt));
  else list = [...all].sort((a, b) => b.views / (1 + ageDays(b) * 0.3) - a.views / (1 + ageDays(a) * 0.3));
  list = list.slice(0, 30);
  const start = cursor ? Number(cursor) : 0;
  const next = start + PAGE;
  return { items: list.slice(start, next), nextCursor: next < list.length ? String(next) : null };
}

export async function mockPopularCreators(): Promise<ChannelResult[]> {
  await wait(250);
  return channels;
}
