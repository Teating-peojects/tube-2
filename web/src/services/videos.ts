import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { Paginated, Reaction, Video, VideoDetail } from "@/types";
import { api } from "./api";
import { mockGetVideos } from "./mock/data";
import { mockLiveNow, mockShorts } from "./mock/misc";
import { mockGetVideo, mockSetReaction, mockSetSubscription } from "./mock/watch";

export type VideoQuery = { cursor?: string; category?: string };

export async function getVideos(q: VideoQuery = {}): Promise<Paginated<Video>> {
  if (env.useMock) return mockGetVideos(q);
  const params = new URLSearchParams();
  if (q.cursor) params.set("cursor", q.cursor);
  if (q.category && q.category !== "All") params.set("category", q.category.toLowerCase());
  return api<Paginated<Video>>(`${endpoints.videos}?${params.toString()}`);
}

export async function getVideo(id: string, init?: RequestInit): Promise<VideoDetail> {
  if (env.useMock) return mockGetVideo(id);
  return api<VideoDetail>(endpoints.video(id), init);
}

export async function setReaction(id: string, reaction: Reaction): Promise<void> {
  if (env.useMock) return mockSetReaction(id, reaction);
  await api(endpoints.reaction(id), { method: "PUT", body: JSON.stringify({ reaction }) });
}

export async function setSubscription(channelId: string, subscribed: boolean): Promise<void> {
  if (env.useMock) return mockSetSubscription(channelId, subscribed);
  await api(endpoints.subscription(channelId), { method: "PUT", body: JSON.stringify({ subscribed }) });
}


export const getShorts = (cursor?: string): Promise<Paginated<Video>> =>
  env.useMock ? mockShorts(cursor) : api(`${endpoints.shorts}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`);
export const getLive = (cursor?: string): Promise<Paginated<Video>> =>
  env.useMock ? mockLiveNow(cursor) : api(`${endpoints.live}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`);
