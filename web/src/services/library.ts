import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { ChannelResult, HistoryItem, Paginated, Video } from "@/types";
import { api } from "./api";
import * as m from "./mock/library";

const withCursor = (path: string, cursor?: string) => (cursor ? `${path}?cursor=${encodeURIComponent(cursor)}` : path);

export const getWatchLaterIds = (): Promise<string[]> => (env.useMock ? m.mockWatchLaterIds() : api(endpoints.watchLaterIds));
export const getWatchLater = (cursor?: string): Promise<Paginated<Video>> => (env.useMock ? m.mockWatchLater(cursor) : api(withCursor(endpoints.watchLater, cursor)));
export const setWatchLater = (videoId: string, add: boolean): Promise<void> =>
  env.useMock ? m.mockSetWatchLater(videoId, add) : api(endpoints.watchLaterItem(videoId), { method: add ? "PUT" : "DELETE" });

export const getHistory = (cursor?: string): Promise<Paginated<HistoryItem>> => (env.useMock ? m.mockHistory(cursor) : api(withCursor(endpoints.history, cursor)));
export const removeHistoryItem = (videoId: string): Promise<void> => (env.useMock ? m.mockRemoveHistory(videoId) : api(endpoints.historyItem(videoId), { method: "DELETE" }));
export const clearHistory = (): Promise<void> => (env.useMock ? m.mockClearHistory() : api(endpoints.history, { method: "DELETE" }));

export const getLiked = (cursor?: string): Promise<Paginated<Video>> => (env.useMock ? m.mockLiked(cursor) : api(withCursor(endpoints.liked, cursor)));
export const getSubscribedChannels = (): Promise<ChannelResult[]> => (env.useMock ? m.mockSubChannels() : api(endpoints.subscriptionChannels));
export const getSubscriptionFeed = (cursor?: string): Promise<Paginated<Video>> => (env.useMock ? m.mockSubFeed(cursor) : api(withCursor(endpoints.subscriptionFeed, cursor)));
