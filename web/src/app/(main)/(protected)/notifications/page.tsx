import type { Metadata } from "next";
import { NotificationsView } from "@/features/notifications/NotificationsView";

export const metadata: Metadata = { title: "Notifications", robots: { index: false, follow: false } };

export default function Page() {
  return <NotificationsView />;
}
