import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { Paginated, Period, StudioAnalytics, StudioComment, StudioFilter, StudioOverview, StudioVideo, VideoVisibility } from "@/types";
import { api } from "./api";
import * as m from "./mock/studio";

const qs = (o: Record<string, string | undefined>) => {
  const p = new URLSearchParams();
  Object.entries(o).forEach(([k, v]) => v && p.set(k, v));
  return p.toString();
};

export const getOverview = (period: Period): Promise<StudioOverview> => (env.useMock ? m.mockOverview(period) : api(`${endpoints.studio.overview}?${qs({ period })}`));
export const getAnalytics = (period: Period): Promise<StudioAnalytics> => (env.useMock ? m.mockAnalytics(period) : api(`${endpoints.studio.analytics}?${qs({ period })}`));
export const getStudioVideos = (filter: StudioFilter, cursor?: string): Promise<Paginated<StudioVideo>> =>
  env.useMock ? m.mockStudioVideos(filter, cursor) : api(`${endpoints.studio.videos}?${qs({ filter, cursor })}`);
export const setVideoVisibility = (id: string, visibility: VideoVisibility): Promise<void> =>
  env.useMock ? m.mockSetVisibility(id, visibility) : api(endpoints.studio.video(id), { method: "PATCH", body: JSON.stringify({ visibility }) });
export const deleteStudioVideo = (id: string): Promise<void> => (env.useMock ? m.mockDeleteStudioVideo(id) : api(endpoints.studio.video(id), { method: "DELETE" }));
export const getStudioComments = (filter: "all" | "held", cursor?: string): Promise<Paginated<StudioComment>> =>
  env.useMock ? m.mockStudioComments(filter, cursor) : api(`${endpoints.studio.comments}?${qs({ filter, cursor })}`);
export const approveComment = (id: string): Promise<void> => (env.useMock ? m.mockApproveComment(id) : api(endpoints.studio.commentApprove(id), { method: "POST" }));
export const deleteComment = (id: string): Promise<void> => (env.useMock ? m.mockDeleteComment(id) : api(endpoints.studio.commentDelete(id), { method: "DELETE" }));
