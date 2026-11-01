import type { Metadata } from "next";
import { SubscriptionsView } from "@/features/library/SubscriptionsView";

export const metadata: Metadata = { title: "Subscriptions", robots: { index: false, follow: false } };

export default function Page() {
  return <SubscriptionsView />;
}
