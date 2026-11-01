import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import { api } from "./api";

/** Counts a view. Fire and forget: analytics must never break playback. */
export async function recordView(videoId: string): Promise<void> {
  if (env.useMock) return;
  try {
    await api(endpoints.videoView(videoId), { method: "POST", keepalive: true });
  } catch { /* ignore */ }
}

/** Saves the resume position and watch time (signed-in viewers only; the server caps watchedSec per call). */
export async function saveProgress(videoId: string, positionSec: number, watchedSec: number): Promise<void> {
  if (env.useMock) return;
  try {
    await api(endpoints.videoProgress(videoId), {
      method: "POST",
      keepalive: true, // lets the request finish while the tab is closing
      body: JSON.stringify({ positionSec: Math.max(0, Math.floor(positionSec)), watchedSec: Math.max(0, Math.floor(watchedSec)) }),
    });
  } catch { /* ignore */ }
}
