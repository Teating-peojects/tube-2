import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { ReportReason, ReportTarget } from "@/types";
import { api } from "./api";
import { defaultReasons, mockNoop, mockReasons, mockReport } from "./mock/misc";

/** Reasons come from the backend config; if that fails the built-in list keeps reporting available. */
export async function getReportReasons(): Promise<ReportReason[]> {
  if (env.useMock) return mockReasons();
  try {
    return await api<ReportReason[]>(endpoints.reportReasons);
  } catch {
    return defaultReasons;
  }
}

export const submitReport = (v: { target: ReportTarget; reason: string; details?: string }): Promise<void> =>
  env.useMock ? mockReport(v) : api(endpoints.reports, { method: "POST", body: JSON.stringify({ targetType: v.target.type, targetId: v.target.id, reason: v.reason, details: v.details }) });

export const markNotInterested = (videoId: string): Promise<void> =>
  env.useMock ? mockNoop() : api(endpoints.notInterested(videoId), { method: "POST" });

export const setChannelBlocked = (channelId: string, blocked: boolean): Promise<void> =>
  env.useMock ? mockNoop() : api(endpoints.blockChannel(channelId), { method: blocked ? "PUT" : "DELETE" });
