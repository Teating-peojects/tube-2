import type { Metadata } from "next";
import { NotificationsPanel } from "@/features/settings/panels";

export const metadata: Metadata = { title: "Notifications settings", robots: { index: false, follow: false } };

export default function Page() {
  return <NotificationsPanel />;
}
