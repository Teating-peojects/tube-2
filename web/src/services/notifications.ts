import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { AppNotification, Paginated } from "@/types";
import { api } from "./api";
import { mockMarkRead, mockNotifications, mockUnreadCount } from "./mock/library";

export function getNotifications(p: { cursor?: string; unreadOnly?: boolean }): Promise<Paginated<AppNotification>> {
  if (env.useMock) return mockNotifications(p);
  const qs = new URLSearchParams();
  if (p.cursor) qs.set("cursor", p.cursor);
  if (p.unreadOnly) qs.set("unread", "1");
  return api(`${endpoints.notifications}?${qs}`);
}
export const markNotificationsRead = (p: { ids?: string[]; all?: boolean }): Promise<void> =>
  env.useMock ? mockMarkRead(p) : api(endpoints.notificationsRead, { method: "POST", body: JSON.stringify(p) });
export async function getUnreadCount(): Promise<number> {
  if (env.useMock) return mockUnreadCount();
  return (await api<{ count: number }>(endpoints.notificationsUnread)).count;
}
