import type { Paginated, ReportReason, ReportTarget, Settings, SettingsPatch, Video } from "@/types";
import { ApiError } from "../api";
import { all } from "./data";

const wait = (ms = 300) => new Promise((r) => setTimeout(r, ms));
function page<T>(list: T[], cursor?: string, size = 8): Paginated<T> {
  const s = cursor ? Number(cursor) : 0;
  return { items: list.slice(s, s + size), nextCursor: s + size < list.length ? String(s + size) : null };
}

export async function mockShorts(cursor?: string): Promise<Paginated<Video>> {
  await wait();
  return page(all.filter((v) => !v.isLive && v.durationSec < 900), cursor, 6);
}
export async function mockLiveNow(cursor?: string): Promise<Paginated<Video>> {
  await wait();
  return page(all.filter((v) => v.isLive), cursor, 12);
}

export const defaultReasons: ReportReason[] = [
  { id: "non_consensual", label: "Non-consensual content", description: "Shared without the consent of the people shown" },
  { id: "underage", label: "Underage / minor concern", description: "Anyone who may be under 18" },
  { id: "copyright", label: "Copyright", description: "Uses my work without permission" },
  { id: "harassment", label: "Harassment", description: "Targets or threatens a person" },
  { id: "spam", label: "Spam or scam", description: "Misleading, repetitive or deceptive" },
  { id: "other", label: "Other" },
];
const reported = new Set<string>();
export async function mockReasons() { await wait(150); return defaultReasons; }
export async function mockReport(v: { target: ReportTarget; reason: string }) {
  await wait(500);
  const k = `${v.target.type}:${v.target.id}`;
  if (reported.has(k)) throw new ApiError(409, "", "already_reported");
  reported.add(k);
}

let settings: Settings = {
  privacy: { showSubscriptions: false, showLikedVideos: false, saveWatchHistory: true },
  notifications: { newVideo: true, replies: true, newSubscriber: true, system: true, email: false },
};
export async function mockGetSettings(): Promise<Settings> { await wait(200); return settings; }
export async function mockPatchSettings(p: SettingsPatch): Promise<void> {
  await wait(250);
  settings = { privacy: { ...settings.privacy, ...p.privacy }, notifications: { ...settings.notifications, ...p.notifications } };
}
export async function mockNoop() { await wait(250); }
