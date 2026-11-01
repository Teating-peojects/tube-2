import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { ChannelResult, Paginated, TrendingTab, Video } from "@/types";
import { api } from "./api";
import { mockPopularCreators, mockTrending } from "./mock/discover";

export async function getTrending(tab: TrendingTab, cursor?: string): Promise<Paginated<Video>> {
  if (env.useMock) return mockTrending(tab, cursor);
  const qs = new URLSearchParams({ tab });
  if (cursor) qs.set("cursor", cursor);
  return api<Paginated<Video>>(`${endpoints.trending}?${qs}`);
}

export async function getPopularCreators(): Promise<ChannelResult[]> {
  if (env.useMock) return mockPopularCreators();
  return api<ChannelResult[]>(endpoints.popularCreators);
}
