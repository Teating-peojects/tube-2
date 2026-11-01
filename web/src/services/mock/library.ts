import type {
  AppNotification, ChannelDetail, ChannelResult, ChannelVideoKind, HistoryItem, NotificationType, Paginated,
  Playlist, PlaylistDetail, PlaylistWithMembership, Video, Visibility,
} from "@/types";
import { ApiError } from "../api";
import { all } from "./data";
import { channels, playlists as publicSeed } from "./discover";
import { reactions, subs } from "./watch";

const wait = (ms = 300) => new Promise((r) => setTimeout(r, ms));
function page<T>(list: T[], cursor?: string, size = 12): Paginated<T> {
  const s = cursor ? Number(cursor) : 0;
  const n = s + size;
  return { items: list.slice(s, n), nextCursor: n < list.length ? String(n) : null };
}
const vid = (id: string) => all.find((v) => v.id === id);
const vids = (ids: string[]) => ids.map(vid).filter((v): v is Video => Boolean(v));

// ---- Watch later / history / liked / subscriptions ----
const wl = new Set<string>(["v2", "v5"]);
let history: HistoryItem[] = all.slice(0, 14).map((v, i) => ({
  video: v,
  watchedAt: new Date(Date.now() - (i + 1) * 7200000).toISOString(),
  progressSec: Math.floor(v.durationSec * [0.35, 0.8, 0.1, 1, 0.5][i % 5]!),
}));

export async function mockWatchLaterIds() { await wait(150); return [...wl]; }
export async function mockWatchLater(cursor?: string) { await wait(); return page(vids([...wl].reverse()), cursor); }
export async function mockSetWatchLater(id: string, add: boolean) { await wait(200); if (add) wl.add(id); else wl.delete(id); }
export async function mockHistory(cursor?: string) { await wait(); return page(history, cursor); }
export async function mockRemoveHistory(id: string) { await wait(200); history = history.filter((h) => h.video.id !== id); }
export async function mockClearHistory() { await wait(250); history = []; }
export async function mockLiked(cursor?: string) {
  await wait();
  return page(vids([...reactions].filter(([, r]) => r === "like").map(([id]) => id)), cursor);
}
export async function mockSubChannels(): Promise<ChannelResult[]> { await wait(200); return channels.filter((c) => subs.has(c.id)); }
export async function mockSubFeed(cursor?: string) {
  await wait();
  const list = all.filter((v) => subs.has(v.creator.id)).sort((a, b) => +new Date(b.publishedAt) - +new Date(a.publishedAt));
  return page(list, cursor);
}

// ---- Playlists ----
type Rec = Playlist & { ownerId: string; videoIds: string[] };
const now = () => new Date().toISOString();
let recs: Rec[] = [
  { id: "mp1", title: "Favorites", description: "", visibility: "private", videoCount: 0, thumbnailUrl: null, ownerName: "Demo User", updatedAt: now(), ownerId: "me", videoIds: ["v1", "v3", "v6"] },
  { id: "mp2", title: "Watch with friends", description: "Picks to share.", visibility: "public", videoCount: 0, thumbnailUrl: null, ownerName: "Demo User", updatedAt: now(), ownerId: "me", videoIds: ["v2", "v4"] },
  ...publicSeed.map((p, i): Rec => ({
    id: p.id, title: p.title, description: "", visibility: "public", videoCount: 0, thumbnailUrl: null,
    ownerName: p.creatorName, updatedAt: now(), ownerId: `owner-${p.creatorName}`, videoIds: all.slice(i, i + 5).map((v) => v.id),
  })),
];
const pub = (r: Rec): Playlist => ({ id: r.id, title: r.title, description: r.description, visibility: r.visibility, videoCount: r.videoIds.length, thumbnailUrl: r.thumbnailUrl, ownerName: r.ownerName, updatedAt: r.updatedAt });
const find = (id: string) => { const r = recs.find((x) => x.id === id); if (!r) throw new ApiError(404, "", "not_found"); return r; };

export async function mockMyPlaylists(videoId?: string): Promise<PlaylistWithMembership[]> {
  await wait(200);
  return recs.filter((r) => r.ownerId === "me").map((r) => ({ ...pub(r), contains: videoId ? r.videoIds.includes(videoId) : false }));
}
export async function mockPlaylist(id: string): Promise<PlaylistDetail> {
  await wait(250);
  const r = find(id);
  if (r.visibility === "private" && r.ownerId !== "me") throw new ApiError(404, "", "not_found");
  return { ...pub(r), isOwner: r.ownerId === "me", videos: vids(r.videoIds) };
}
export async function mockCreatePlaylist(v: { title: string; visibility: Visibility; videoId?: string }): Promise<Playlist> {
  await wait(250);
  const r: Rec = { id: `mp-${Date.now()}`, title: v.title, description: "", visibility: v.visibility, videoCount: 0, thumbnailUrl: null, ownerName: "Demo User", updatedAt: now(), ownerId: "me", videoIds: v.videoId ? [v.videoId] : [] };
  recs = [r, ...recs];
  return pub(r);
}
export async function mockUpdatePlaylist(id: string, patch: Partial<Pick<Playlist, "title" | "description" | "visibility">>) {
  await wait(200);
  Object.assign(find(id), patch, { updatedAt: now() });
}
export async function mockDeletePlaylist(id: string) { await wait(250); recs = recs.filter((r) => r.id !== id); }
export async function mockSetPlaylistVideo(id: string, videoId: string, add: boolean) {
  await wait(200);
  const r = find(id);
  r.videoIds = add ? [...new Set([...r.videoIds, videoId])] : r.videoIds.filter((x) => x !== videoId);
}
export async function mockReorder(id: string, ids: string[]) { await wait(200); find(id).videoIds = ids; }
export async function mockChannelPlaylists(channelId: string): Promise<Playlist[]> {
  await wait(250);
  const ch = channels.find((c) => c.id === channelId);
  return recs.filter((r) => r.visibility === "public" && r.ownerName === ch?.name).map(pub);
}

// ---- Channels ----
export async function mockChannel(id: string): Promise<ChannelDetail> {
  await wait(250);
  const c = channels.find((x) => x.id === id);
  if (!c) throw new ApiError(404, "", "not_found");
  const sub = subs.has(id);
  return { ...c, isSubscribed: sub, subscribers: c.subscribers + (sub ? 1 : 0), bannerUrl: null, joinedAt: "2024-03-12T00:00:00Z", totalViews: 4_200_000 + c.subscribers * 3, links: [{ label: "Website", url: "https://example.com" }] };
}
export async function mockChannelVideos(id: string, p: { kind: ChannelVideoKind; sort: "latest" | "popular"; cursor?: string }) {
  await wait(300);
  let list = all.filter((v) => v.creator.id === id);
  list = p.kind === "live" ? list.filter((v) => v.isLive) : p.kind === "shorts" ? list.filter((v) => !v.isLive && v.durationSec < 480) : list.filter((v) => !v.isLive && v.durationSec >= 480);
  list = [...list].sort((a, b) => (p.sort === "popular" ? b.views - a.views : +new Date(b.publishedAt) - +new Date(a.publishedAt)));
  return page(list, p.cursor);
}

// ---- Notifications ----
const kinds: [NotificationType, string, string, string][] = [
  ["new_subscriber", "New subscriber", "Someone subscribed to your channel.", "/studio"],
  ["creator_upload", "New video from Creator Name", "Neon City Nights: A Cinematic Drive", "/watch/v1"],
  ["comment_reply", "New reply to your comment", "“Thanks for watching!”", "/watch/v3"],
  ["new_comment", "New comment on your video", "“Great quality, thanks for sharing.”", "/watch/v2"],
  ["processing_completed", "Your video finished processing", "It’s ready to publish.", "/studio/videos"],
  ["system", "Community guidelines updated", "Review the latest changes.", "/community-guidelines"],
  ["processing_failed", "Video processing failed", "Re-upload the file to try again.", "/studio/videos"],
];
let notifs: AppNotification[] = Array.from({ length: 26 }, (_, i) => {
  const [type, title, body, href] = kinds[i % kinds.length]!;
  return { id: `n${i + 1}`, type, title, body, href, createdAt: new Date(Date.now() - (i + 1) * 5400000).toISOString(), read: i >= 6 };
});
export async function mockNotifications(p: { cursor?: string; unreadOnly?: boolean }) {
  await wait();
  return page(p.unreadOnly ? notifs.filter((n) => !n.read) : notifs, p.cursor, 10);
}
export async function mockMarkRead(p: { ids?: string[]; all?: boolean }) {
  await wait(150);
  notifs = notifs.map((n) => (p.all || p.ids?.includes(n.id) ? { ...n, read: true } : n));
}
export async function mockUnreadCount() { await wait(100); return notifs.filter((n) => !n.read).length; }
