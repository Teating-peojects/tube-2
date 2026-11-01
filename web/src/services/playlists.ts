import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { Playlist, PlaylistDetail, PlaylistWithMembership, Visibility } from "@/types";
import { api } from "./api";
import * as m from "./mock/library";

const json = (method: string, body?: unknown): RequestInit => ({ method, body: body === undefined ? undefined : JSON.stringify(body) });

export const getMyPlaylists = (videoId?: string): Promise<PlaylistWithMembership[]> =>
  env.useMock ? m.mockMyPlaylists(videoId) : api(`${endpoints.playlists}${videoId ? `?videoId=${encodeURIComponent(videoId)}` : ""}`);
export const getPlaylist = (id: string, init?: RequestInit): Promise<PlaylistDetail> =>
  env.useMock ? m.mockPlaylist(id) : api(endpoints.playlist(id), init);
export const createPlaylist = (v: { title: string; visibility: Visibility; videoId?: string }): Promise<Playlist> =>
  env.useMock ? m.mockCreatePlaylist(v) : api(endpoints.playlists, json("POST", v));
export const updatePlaylist = (id: string, patch: Partial<Pick<Playlist, "title" | "description" | "visibility">>): Promise<void> =>
  env.useMock ? m.mockUpdatePlaylist(id, patch) : api(endpoints.playlist(id), json("PATCH", patch));
export const deletePlaylist = (id: string): Promise<void> => (env.useMock ? m.mockDeletePlaylist(id) : api(endpoints.playlist(id), json("DELETE")));
export const setPlaylistVideo = (id: string, videoId: string, add: boolean): Promise<void> =>
  env.useMock ? m.mockSetPlaylistVideo(id, videoId, add) : api(endpoints.playlistVideo(id, videoId), json(add ? "PUT" : "DELETE"));
export const reorderPlaylist = (id: string, videoIds: string[]): Promise<void> =>
  env.useMock ? m.mockReorder(id, videoIds) : api(endpoints.playlistOrder(id), json("PUT", { videoIds }));
