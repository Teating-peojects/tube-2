import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { ChannelDetail, ChannelVideoKind, Paginated, Playlist, Video } from "@/types";
import { api } from "./api";
import { mockChannel, mockChannelPlaylists, mockChannelVideos } from "./mock/library";

export const getChannel = (id: string, init?: RequestInit): Promise<ChannelDetail> =>
  env.useMock ? mockChannel(id) : api(endpoints.channel(id), init);

export type ChannelVideosQuery = { kind: ChannelVideoKind; sort: "latest" | "popular"; cursor?: string };
export function getChannelVideos(id: string, q: ChannelVideosQuery): Promise<Paginated<Video>> {
  if (env.useMock) return mockChannelVideos(id, q);
  const qs = new URLSearchParams({ kind: q.kind, sort: q.sort });
  if (q.cursor) qs.set("cursor", q.cursor);
  return api(`${endpoints.channelVideos(id)}?${qs}`);
}

export const getChannelPlaylists = (id: string): Promise<Playlist[]> =>
  env.useMock ? mockChannelPlaylists(id) : api(endpoints.channelPlaylists(id));
