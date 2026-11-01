import type { Comment, CommentSort, Paginated, Reaction, VideoDetail } from "@/types";
import { ApiError } from "../api";
import { all } from "./data";

// Public HLS test stream so the player can be exercised without a backend.
const STREAM = "https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8";
const wait = (ms = 300) => new Promise((r) => setTimeout(r, ms));
const seed = (id: string) => [...id].reduce((a, c) => a + c.charCodeAt(0), 0);

export const reactions = new Map<string, Reaction>();
export const subs = new Set<string>();

export async function mockGetVideo(id: string): Promise<VideoDetail> {
  await wait(250);
  const v = all.find((x) => x.id === id);
  if (!v) throw new ApiError(404, "Not found");
  const s = seed(id);
  const r = reactions.get(id) ?? null;
  const sub = subs.has(v.creator.id);
  return {
    ...v,
    creator: { ...v.creator, subscribers: 120000 + s * 91 + (sub ? 1 : 0), isSubscribed: sub },
    description: "Sample description for this video.\nAdd details, credits and links here.",
    streamUrl: STREAM,
    posterUrl: null,
    captions: [],
    likes: 800 + s * 13 + (r === "like" ? 1 : 0),
    dislikes: 20 + (s % 50) + (r === "dislike" ? 1 : 0),
    reaction: r,
    commentCount: 28,
  };
}

export async function mockSetReaction(id: string, r: Reaction) {
  await wait(250);
  reactions.set(id, r);
}
export async function mockSetSubscription(channelId: string, subscribed: boolean) {
  await wait(250);
  if (subscribed) subs.add(channelId); else subs.delete(channelId);
}

const texts = [
  "Great quality, thanks for sharing.", "Really well produced. Looking forward to more.",
  "The pacing on this one is perfect.", "Subscribed. Keep it up!", "Can you share the settings you used?",
  "Underrated channel, deserves more views.", "Watched this twice already.",
];
const added = new Map<string, Comment[]>();
const addedReplies = new Map<string, Comment[]>();

function base(videoId: string): Comment[] {
  const s = seed(videoId);
  return Array.from({ length: 28 }, (_, i) => ({
    id: `${videoId}-c${i + 1}`,
    author: { id: `u${i}`, name: i === 0 ? "Creator Name" : `User ${i + 1}`, avatarUrl: null, isCreator: i === 0 },
    text: texts[i % texts.length] ?? "",
    createdAt: new Date(Date.now() - (i + 1) * 5 * 3600000).toISOString(),
    likes: (s * (i + 3)) % 500,
    liked: false,
    replyCount: i % 4 === 1 ? 2 : 0,
    pinned: i === 0,
  }));
}

export async function mockGetComments(videoId: string, p: { cursor?: string; sort?: CommentSort }): Promise<Paginated<Comment>> {
  await wait();
  const list = [...(added.get(videoId) ?? []), ...base(videoId)];
  list.sort((a, b) => Number(b.pinned) - Number(a.pinned) ||
    (p.sort === "new" ? +new Date(b.createdAt) - +new Date(a.createdAt) : b.likes - a.likes));
  const start = p.cursor ? Number(p.cursor) : 0;
  const next = start + 10;
  return { items: list.slice(start, next), nextCursor: next < list.length ? String(next) : null };
}

export async function mockGetReplies(commentId: string): Promise<Paginated<Comment>> {
  await wait(250);
  const seeded: Comment[] = Array.from({ length: 2 }, (_, i) => ({
    id: `${commentId}-r${i + 1}`,
    author: { id: `r${i}`, name: `User ${i + 40}`, avatarUrl: null, isCreator: false },
    text: texts[(i + 3) % texts.length] ?? "",
    createdAt: new Date(Date.now() - (i + 1) * 3600000).toISOString(),
    likes: i * 3, liked: false, replyCount: 0, pinned: false,
  }));
  return { items: [...seeded, ...(addedReplies.get(commentId) ?? [])], nextCursor: null };
}

export async function mockCreateComment(videoId: string, text: string, parentId?: string): Promise<Comment> {
  await wait(300);
  const c: Comment = {
    id: `new-${Date.now()}`, author: { id: "me", name: "You", avatarUrl: null, isCreator: false },
    text, createdAt: new Date().toISOString(), likes: 0, liked: false, replyCount: 0, pinned: false,
  };
  const store = parentId ? addedReplies : added;
  const key = parentId ?? videoId;
  store.set(key, [...(store.get(key) ?? []), c]);
  return c;
}
