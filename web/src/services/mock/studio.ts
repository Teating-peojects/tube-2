import type {
  Paginated, Period, SeriesPoint, ServerUploadStatus, StudioAnalytics, StudioComment, StudioFilter,
  StudioOverview, StudioVideo, UploadInit, UploadMeta, VideoVisibility,
} from "@/types";
import { UploadError, type PutPart } from "@/lib/uploader";
import { ApiError } from "../api";
import { all } from "./data";

const wait = (ms = 300) => new Promise((r) => setTimeout(r, ms));
function page<T>(list: T[], cursor?: string, size = 10): Paginated<T> {
  const s = cursor ? Number(cursor) : 0;
  return { items: list.slice(s, s + size), nextCursor: s + size < list.length ? String(s + size) : null };
}

// ---- Upload (simulated) ----
const sessions = new Map<string, { t0: number | null; published: boolean }>();

export const mockPut: PutPart = (blob, _url, onProgress, signal) =>
  new Promise((resolve, reject) => {
    let loaded = 0;
    const tick = setInterval(() => {
      loaded = Math.min(blob.size, loaded + 40e6 * 0.1 * (0.7 + Math.random() * 0.6));
      onProgress(loaded);
      if (loaded >= blob.size) { clearInterval(tick); resolve(`etag-${Math.random().toString(36).slice(2, 10)}`); }
    }, 100);
    if (signal.aborted) { clearInterval(tick); return reject(new UploadError("aborted")); }
    signal.addEventListener("abort", () => { clearInterval(tick); reject(new UploadError("aborted")); }, { once: true });
  });

export async function mockInit(v: { size: number }): Promise<UploadInit> {
  await wait(400);
  const uploadId = `up-${Date.now()}`;
  const partSize = Math.max(5 * 1024 ** 2, Math.ceil(v.size / 1000));
  const n = Math.max(1, Math.ceil(v.size / partSize));
  sessions.set(uploadId, { t0: null, published: false });
  return { uploadId, videoId: `new-${uploadId}`, partSize, parts: Array.from({ length: n }, (_, i) => ({ partNumber: i + 1, url: `mock://${uploadId}/${i + 1}` })) };
}
export async function mockComplete(id: string) { await wait(300); const s = sessions.get(id); if (s) s.t0 = Date.now(); }
export async function mockStatus(id: string): Promise<ServerUploadStatus> {
  await wait(150);
  const s = sessions.get(id);
  if (!s?.t0) throw new ApiError(404, "", "not_found");
  if (s.published) return { status: "published", progress: 100, thumbnails: [] };
  const dt = (Date.now() - s.t0) / 1000;
  if (dt < 4) return { status: "processing", progress: Math.round((dt / 4) * 100), thumbnails: [] };
  if (dt < 9) return { status: "encoding", progress: Math.round(((dt - 4) / 5) * 100), thumbnails: [] };
  return { status: "ready", progress: 100, thumbnails: [] };
}
export async function mockAsset() { await wait(250); return `asset-${Date.now()}`; }
export async function mockSaveDraft() { await wait(300); }
export async function mockPublish(videoId: string, meta: UploadMeta) {
  await wait(500);
  const s = sessions.get(videoId.replace(/^new-/, ""));
  if (s) s.published = true;
  studioVideos = [{ id: videoId, title: meta.title, thumbnailUrl: null, durationSec: 0, status: "published", visibility: meta.visibility, createdAt: new Date().toISOString(), views: 0, comments: 0, likes: 0 }, ...studioVideos];
}

// ---- Studio videos ----
let studioVideos: StudioVideo[] = all.slice(0, 18).map((v, i) => ({
  id: v.id, title: v.title, thumbnailUrl: null, durationSec: v.durationSec,
  status: i % 7 === 3 ? "draft" : i % 9 === 4 ? "processing" : "published",
  visibility: (["public", "public", "unlisted", "private"] as const)[i % 4]!,
  createdAt: v.publishedAt, views: i % 7 === 3 || i % 9 === 4 ? 0 : v.views, comments: (i * 3) % 40, likes: (i * 17) % 900,
}));

export async function mockStudioVideos(filter: StudioFilter, cursor?: string) {
  await wait();
  const f = studioVideos.filter((v) =>
    filter === "all" ? true : filter === "published" ? v.status === "published" && v.visibility === "public"
      : filter === "draft" || filter === "processing" ? v.status === filter : v.visibility === filter);
  return page(f, cursor);
}
export async function mockSetVisibility(id: string, visibility: VideoVisibility) {
  await wait(200);
  studioVideos = studioVideos.map((v) => (v.id === id ? { ...v, visibility } : v));
}
export async function mockDeleteStudioVideo(id: string) { await wait(250); studioVideos = studioVideos.filter((v) => v.id !== id); }

// ---- Overview / analytics ----
const DAYS: Record<Period, number> = { "7d": 7, "28d": 28, "90d": 90 };
function series(period: Period, base: number, k: number): SeriesPoint[] {
  const n = DAYS[period];
  return Array.from({ length: n }, (_, i) => ({
    date: new Date(Date.now() - (n - 1 - i) * 86400000).toISOString().slice(0, 10),
    value: Math.max(0, Math.round(base + k * Math.sin(i / 3) * base * 0.3 + i * base * 0.01)),
  }));
}
const sum = (s: SeriesPoint[]) => s.reduce((a, b) => a + b.value, 0);

export async function mockOverview(period: Period): Promise<StudioOverview> {
  await wait();
  const views = series(period, 1200, 1);
  return {
    period, views,
    totals: { views: sum(views), watchTimeHours: Math.round(sum(views) * 0.045), subscribers: 5240, subscribersDelta: DAYS[period] * 6, revenue: null },
    topVideos: [...all].sort((a, b) => b.views - a.views).slice(0, 5).map((v) => ({ id: v.id, title: v.title, views: v.views })),
  };
}
export async function mockAnalytics(period: Period): Promise<StudioAnalytics> {
  await wait();
  return {
    period, views: series(period, 1200, 1), watchTime: series(period, 55, 1.4),
    audience: {
      countries: [{ label: "United States", value: 38 }, { label: "United Kingdom", value: 14 }, { label: "Germany", value: 9 }, { label: "Brazil", value: 8 }, { label: "India", value: 7 }],
      devices: [{ label: "Mobile", value: 62 }, { label: "Desktop", value: 31 }, { label: "Tablet", value: 5 }, { label: "TV", value: 2 }],
    },
    traffic: [{ label: "Home feed", value: 34 }, { label: "Search", value: 26 }, { label: "Subscriptions", value: 21 }, { label: "Direct / other", value: 12 }, { label: "External", value: 7 }],
    engagement: { likes: 8420, comments: 1310, shares: 640, avgViewDurationSec: 412 },
  };
}

// ---- Comments moderation ----
const texts = ["Great quality, thanks for sharing.", "Can you share the settings you used?", "Subscribed. Keep it up!", "Watched this twice already.", "Underrated channel, deserves more views."];
let sComments: StudioComment[] = Array.from({ length: 24 }, (_, i) => ({
  id: `sc${i + 1}`, videoId: all[i % 8]!.id, videoTitle: all[i % 8]!.title, author: `User ${i + 2}`, text: texts[i % texts.length]!,
  createdAt: new Date(Date.now() - (i + 1) * 4 * 3600000).toISOString(), status: i % 4 === 0 ? "held" : "published",
}));
export async function mockStudioComments(filter: "all" | "held", cursor?: string) {
  await wait();
  return page(filter === "held" ? sComments.filter((c) => c.status === "held") : sComments, cursor);
}
export async function mockApproveComment(id: string) { await wait(200); sComments = sComments.map((c) => (c.id === id ? { ...c, status: "published" } : c)); }
export async function mockDeleteComment(id: string) { await wait(200); sComments = sComments.filter((c) => c.id !== id); }
